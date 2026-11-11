# ADR-76919: Honor `--no-gitattributes` in Dependency Compilation

**Date**: 2026-11-11
**Status**: Accepted
**Deciders**: pelikhan, copilot-swe-agent

---

### Context

`gh aw add` compiles the added workflow and any local `.md` dependencies it pulls in: `dispatch-workflow` targets and `call-workflow` workers. Each compile ends with `ensureGitAttributes()`, which adds the `linguist-generated` entry for `.lock.yml` files to `.gitattributes`. The add step already honors `--no-gitattributes`, but the compile steps called `ensureGitAttributes()` unconditionally, so `.gitattributes` could still be created or modified when the flag was set. The existing unit tests logged errors instead of failing and did not exercise dependencies, so the gap went unnoticed (see issue #76915).

### Decision

We will pass `NoGitattributes` through the per-call options structs (`trackedWorkflowCompileOptions` and `compileDepsOptions`) and skip `ensureGitAttributes()` when it is set, for the top-level workflow and its dependencies alike. The add pipeline will create a file tracker when a caller does not provide one, so every add compilation path uses the options-aware compiler. The flag then means "the add command never touches `.gitattributes`," not "the add step skips it." The primary driver is correctness of an explicit user opt-out. The new code paths take options structs rather than longer positional parameter lists, following [ADR-52982](52982-options-struct-for-parameter-heavy-functions.md).

### Alternatives Considered

#### Alternative 1: Add a `noGitattributes bool` parameter to each compile helper

This is the smallest diff and matches the existing style. We did not choose it because `compileWorkflowWithTrackingAndRefreshAndActionRef` already takes eight parameters, and every call site would change again for the next flag. This was a close call on diff size, but the options-struct approach is the pattern already recorded in ADR-52982.

#### Alternative 2: Snapshot and restore `.gitattributes` around dependency compilation

We could let compilation run unchanged and then restore the file's previous contents. We rejected this because it writes and then reverts a file that should not be touched, it must also undo file-tracker bookkeeping, and an interruption can leave a modified file behind. A flag check avoids the write entirely.

### Consequences

#### Positive
- `--no-gitattributes` prevents `.gitattributes` writes in top-level workflow compilation and in dispatch and call-workflow dependency compilation, even when add helpers are called without a tracker.
- Adding a future option to the compile helpers needs one struct field, not a new parameter at every call site.
- Test assertions use `require`, so a failing `addWorkflows` call fails the test instead of being logged. Default-mode append, preservation, idempotency, and legacy-entry migration are now covered.

#### Negative
- Dispatch dependency compilation remains best-effort; call-workflow dependency errors are propagated because a missing worker lock would leave the orchestrator referencing a non-existent file.
- Tests cover both add-level opt-out behavior and dispatch dependency compilation with a nil tracker.
- The tests use `t.Chdir`, which requires Go 1.24 or later, and cannot run in parallel.

#### Neutral
- Default behavior (no flag) is unchanged: every add still appends the lock-file entry.
- A `.gitattributes` write failure is still non-fatal, so the add does not fail because of it.

---

*Accepted for PR #76919.*
