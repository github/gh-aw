# ADR-76620: Flag Discarded Errors from Module-Local Functions

**Date**: 2026-11-10
**Status**: Draft
**Deciders**: Unknown (draft generated from PR #76620; author to confirm)

---

### Context

gh-aw enforces project conventions through a suite of custom `golang.org/x/tools/go/analysis` analyzers under `pkg/linters/`, each registered in `pkg/linters/registry.go`. The repository's `.golangci.yml` disables `errcheck`, with a comment noting that its `exclude-functions` setting does not work properly under golangci-lint v2, so ordinary discarded error returns are not caught by the standard toolchain. The PR body reports that a code-pattern scan found recurring discarded error assignments, which means failures in project functions can pass without notice. The check must stay focused on code the project owns, must not flag every standard-library call, and must honor generated-file skipping and `//nolint` suppression like sibling analyzers.

### Decision

We will add a standalone analyzer, `ignoredlocalerror` (`pkg/linters/ignoredlocalerror`), that reports an error result discarded by a bare expression statement or a blank assignment when the callee belongs to the `github.com/github/gh-aw` module or one of its subpackages. The primary driver is closing the error-discard gap left by the disabled `errcheck` while keeping diagnostics limited to project-owned contracts. The analyzer is registered in `pkg/linters/registry.go`, documented alongside the other analyzers, and listed in `notYetEnforced` in `doc_sync_test.go`, so it is not yet enforced in CI until existing violations are reviewed.

### Alternatives Considered

#### Alternative 1: Re-enable `errcheck` in golangci-lint

Re-enabling the off-the-shelf `errcheck` linter would cover this pattern with no new code to maintain. It was rejected for now because `.golangci.yml` disables it, and its `exclude-functions` entries (such as `(*os.File).Close`) do not take effect under golangci-lint v2, which makes the standard-library noise hard to trim. This was a close call; it should be revisited if the exclusion behavior is fixed upstream. [TODO: verify the current golangci-lint v2 status.]

#### Alternative 2: Flag every error-returning call, not only module-local ones

A broader analyzer would also catch discarded errors from the standard library and third-party packages. It was rejected because it would need a large allow-list and would overlap existing targeted analyzers such as `closeerrorunchecked`, `osremoveuncheckedcleanup`, `jsonmarshalignoredeerror`, and `strconvparseignorederror`. Scoping to the module keeps the signal on contracts the project controls. This is also a close call, because third-party discards remain uncovered.

### Consequences

#### Positive
- Discarded errors from gh-aw functions become visible to the linter suite, covering both bare calls and `_`-assigned results.
- The analyzer uses the shared helpers (`analyzerutil`, `filecheck`, `nolint`), so generated-file skipping and `//nolint:ignoredlocalerror` behave like sibling analyzers.
- Registration, `README.md`, and `doc.go` are checked by existing sync tests (`doc_sync_test.go`, `spec_test.go`), which limits documentation drift.

#### Negative
- The number of existing violations is not reported by this change. [TODO: verify the count by running the analyzer across the repository.]
- Known blind spots: an error assigned to a named variable and then ignored (for example `_, err := f(); _ = err`) is not reported, as the testdata shows. Calls whose callee type cannot be resolved are skipped.
- Errors from the standard library and third-party packages are out of scope, so the gap that motivated the change is only partly closed.

#### Neutral
- Intentional discards are suppressed per call site with `//nolint:ignoredlocalerror`. Teams must decide case by case whether to annotate or refactor them.
- Each new analyzer requires updates in four places (registry, `README.md`, `doc.go`, `spec_test.go`), the same as its siblings.
- Every lint run gains one more analyzer pass over the codebase, a small increase in lint time.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
