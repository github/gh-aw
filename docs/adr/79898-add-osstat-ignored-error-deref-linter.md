# ADR-79898: Add os.Stat and os.Lstat Ignored-Error Dereference Linter

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: Unknown — PR author `gh-aw-bot`; reviewers [TODO: verify]

---

### Context

Calling `os.Stat` or `os.Lstat` and discarding the error (`info, _ := os.Stat(path)`) can leave `info` nil. Dereferencing it without a nil check (for example, `info.Size()`) panics at runtime. The pattern is easy to write and easy to miss in review. `pkg/cli/token_usage_analyze.go` calls `os.Stat` with a discarded error in several places (lines 27, 51, and 195), and the linked issue #79836 cites it as the motivating pattern. Line 27 currently guards the result with `if fileInfo != nil`. The existing linters in `pkg/linters` cover several other discarded-error APIs, but none flags the later dereference of an `os.Stat` or `os.Lstat` result.

### Decision

We will add a custom `go/analysis` analyzer, `osstatignoredeerrorderef`, that reports a field access or `*` dereference of an `os.Stat` or `os.Lstat` result when the error is discarded and no nil check precedes the use. The analyzer is registered in `pkg/linters/registry.go` and documented in `pkg/linters/README.md` and `pkg/linters/doc.go`. It is listed in `notYetEnforced` in `doc_sync_test.go` until an enforcement-readiness audit is done. The primary driver is correctness: preventing a nil-pointer panic class at lint time.

### Alternatives Considered

#### Alternative 1: Extend an existing discarded-error analyzer

The repo already has per-API analyzers for discarded errors, such as `globwalkignorederror`, `strconvparseignorederror`, and `jsonmarshalignoredeerror`. Extending one of them to cover `os.Stat` would keep the number of analyzers down. This was a close call. The new analyzer reuses the shared `astutil.MatchDiscardedErrorCall` helper, so most of the matching logic is already shared. The deciding factor is that the panic happens at the dereference, not at the discarded error, and a separate analyzer keeps the rule and its fixtures focused on that site. [TODO: verify that the existing analyzers do not already flag dereferences.]

#### Alternative 2: Fix call sites by hand and rely on review

Each affected call site could be fixed directly, with no new linter. This adds no maintenance, but the pattern recurs (`pkg/cli/token_usage_analyze.go` has at least three `os.Stat` calls with discarded errors), and review is unlikely to catch a new instance reliably. We rejected it because the linter prevents recurrence in code that is not yet written.

### Consequences

#### Positive
- Discarded `os.Stat` and `os.Lstat` errors that lead to nil dereferences are reported before they reach runtime.
- The fixtures under `testdata/` cover Stat and Lstat, assignments, `if` initializers, nil guards, retained errors, and non-`os` `Stat` calls. This makes the rule's intended behavior explicit.
- The analyzer follows the existing pattern: `analyzerutil.New`, shared `astutil` helpers, a registry entry, and the doc-sync test.

#### Negative
- Guard detection is syntactic. It recognizes `if x != nil { ... }`, `if x == nil { return }`, and `if x == nil { ... } else { ... }`. Other guard forms, such as a guard in a different scope or a reassignment before use, can produce false positives.
- The analyzer is in `notYetEnforced`, so it gives advisory signal only until the audit completes and CI enforcement is enabled.
- The change adds about 185 lines of analyzer code and updates the analyzer count in `doc.go` and the README. These must stay in sync with future analyzer changes.

#### Neutral
- The PR also regenerates `.github/workflows/daily-doc-updater.lock.yml` and `.github/workflows/spec-librarian.lock.yml`. These changes switch the PageIndex install from a version-pinned `uv pip install` to `uv pip sync --require-hashes` against `.github/workflows/requirements-pageindex.txt`. They are unrelated to this linter decision and should be reviewed separately or split into their own ADR. [TODO: verify the source change for these lock files.]
- The analyzer depends on `astutil.MatchDiscardedErrorCall` and `analyzerutil`, so changes to those helpers also affect this analyzer.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
