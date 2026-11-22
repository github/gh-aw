# ADR-80119: Add Linter for Discarded exec.Cmd Output Stderr

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: Unknown — PR author `gh-aw-bot` (linter-miner workflow); reviewers [TODO: verify]

---

### Context

`exec.Cmd.Output()` returns a `*exec.ExitError` on failure, and that error carries the child process's stderr in its `Stderr` field, but only if the caller reads it. `exec.Cmd.CombinedOutput()` returns stdout and stderr together, so discarding its result loses the diagnostics entirely. Both patterns are easy to write and easy to miss in review. The PR's code-pattern scan found roughly 98 `exec.Command` call sites under `pkg/` and `cmd/`, where command failures can lose stderr. Existing linters in `pkg/linters` cover discarded errors and `exec.Command` context usage, but none checks how the results of `Output()` and `CombinedOutput()` are handled.

### Decision

We will add a custom `go/analysis` analyzer, `execcommandoutputerrdiscard`, that reports `exec.Cmd.Output()` calls whose error neither retains `*exec.ExitError.Stderr` nor propagates the error, and `exec.Cmd.CombinedOutput()` calls whose output is discarded. The analyzer is registered in `pkg/linters/registry.go`, documented in `pkg/linters/README.md` and `pkg/linters/doc.go`, and listed in `notYetEnforced` in `doc_sync_test.go`, so it gives advisory signal only until existing call sites are reviewed. The primary driver is diagnosability: keeping stderr from failed commands so that CLI and CI failures can be explained.

### Alternatives Considered

#### Alternative 1: Enforce the check in CI immediately

The analyzer could block CI from the first day. This gives the strongest guarantee. We did not choose it because the PR records that existing exec output error handling needs review before enforcement, and its full-repository `make golint-custom` run did not complete (a constrained runner killed a Go compiler process). Enforcing now would likely block unrelated PRs on unreviewed call sites. This was a close call. [TODO: verify the current number of violations across `pkg/` and `cmd/`.]

#### Alternative 2: Rely on review and a documented convention

Contributors could be told to always capture stderr from `Output()` and `CombinedOutput()` results, with no new linter. This adds no maintenance cost. We did not choose it because the pattern is easy to write and easy to miss, and roughly 98 call sites suggest review alone will not keep up.

#### Alternative 3: Route all command execution through a shared helper

A single command-runner helper could always capture stderr into the returned error, so callers could not drop it. This removes the problem at the source. The PR does not discuss this option, and it is not chosen here because it requires refactoring many call sites before any protection exists, and it does not stop direct `exec.Cmd` use. [TODO: verify whether such a helper already exists in `pkg/`.]

### Consequences

#### Positive
- Failures from `Output()` and `CombinedOutput()` that discard stderr are reported before merge, which makes CLI and CI failures easier to debug.
- The analyzer resolves `os/exec` methods through `go/types`, so unrelated `Output` methods are not flagged. The fixtures cover this case.
- It follows existing linter conventions: `analyzerutil`, shared `astutil` helpers, `//nolint:execcommandoutputerrdiscard` suppression, generated-file skipping, `analysistest` fixtures, a registry entry, and doc-sync tests.

#### Negative
- The analysis is intraprocedural. It tracks result bindings within the enclosing function body and does not follow results passed to helper functions, so it can miss handling in other functions or report patterns it does not recognize. [TODO: verify the false-positive rate on the full repository.]
- The full-repository run did not complete, so the number of existing violations is unknown from the PR. The analyzer stays advisory until those are remediated.
- The change adds about 636 lines of analyzer code and 245 lines of fixtures. The linter count (100 to 101) must stay synchronized across `doc.go`, `README.md`, `spec_test.go`, and `doc_sync_test.go`.

#### Neutral
- The analyzer is part of the suite but not enforced in CI. Enforcement starts when its entry is removed from `notYetEnforced` after review.
- The original linter-miner draft flagged `README.md` as a protected file. Reviewers should confirm the README edits are intended.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
