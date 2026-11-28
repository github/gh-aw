# 81793. Propagate cancellation through ShellCheck workers and native processes

- **Status:** Draft
- **Date:** 2026-11-28
- **PR:** [#81793](https://github.com/github/gh-aw/pull/81793)
- **Issue:** [#81668](https://github.com/github/gh-aw/issues/81668)

## Context

`runShellcheckWorkers` in `pkg/cli/shellcheck.go` limits concurrency with a semaphore. Before this change, a worker that observed `ctx.Done()` while waiting for a slot returned without recording a result. Its zero-valued `shellcheckResult` (nil error) was then treated as a successful scan by `reportShellcheckResultsWithErrors`, so a partially skipped scan could be reported as clean.

The native ShellCheck path called `exec.Command` without a context, so canceling the scan did not stop a running `shellcheck` process. The Docker path already received the context. Issue #81668 asks for both paths to honor cancellation, with deterministic tests that do not need Docker.

## Decision

Treat cancellation as a scan failure on both the native and worker paths:

1. `runShellcheckOnScript` takes a `context.Context` and starts the native binary with `exec.CommandContext`, so canceling the scan terminates the process.
2. When the context is done after the native process exits with an error, the function returns `fmt.Errorf("shellcheck failed: %w", ctx.Err())` before interpreting the exit status as findings or an operational error.
3. A worker that cannot acquire a semaphore slot before cancellation records `shellcheckResult{err: ctx.Err()}` for its step. The aggregate reporter therefore receives a non-nil error that wraps `context.Canceled` instead of a clean result.

The native and Docker paths now share the same context contract. Findings and operational errors are reported as before.

## Alternatives Considered

1. **Fix only the aggregate reporting and leave native execution without a context.** Queued steps would report cancellation, but a running native `shellcheck` process would keep running until it exits on its own. Rejected because the issue requires prompt termination of native processes, and `TestRunShellcheckOnScriptNativeCancellation` enforces a 2-second bound.
2. **Detect cancellation once in the reporter (a single `ctx.Err()` check after the workers finish) instead of recording an error per step.** This would be a smaller change in one place. The PR records the error on each unscheduled step instead, and its tests assert on each step's `err` (`TestRunShellcheckWorkersCancellationWhileWaiting` requires `context.Canceled` for every step), so the per-step result is the tested contract.

## Consequences

### Positive

- A canceled native scan stops promptly instead of running to completion in the background.
- A canceled scan can no longer be reported as clean. Aggregate reporting returns an error wrapping `context.Canceled`.
- Native and Docker paths share one context contract. The new tests cover clean results, findings, operational errors, queued cancellation, and native termination without Docker.

### Negative

- The signature of `runShellcheckOnScript` changed. All six call sites in `pkg/cli` are updated in this PR, but any future caller must now pass a context.
- Cancellation now surfaces as an error instead of a silent success, so runs that are canceled may fail where they previously passed.
- The native cancellation tests use a POSIX `sh` stub and `sleep`, and they are skipped on Windows. Native cancellation therefore has no coverage on Windows. The 2-second and 5-second timing bounds may also be sensitive to slow CI runners.
