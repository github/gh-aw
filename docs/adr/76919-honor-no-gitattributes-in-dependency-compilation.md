# ADR-76919: Honor `--no-gitattributes` in Dependency Compilation

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: [TODO: verify] AI-generated draft for PR #76919 (PR author `app/copilot-swe-agent`, triggering actor `pelikhan`)

---

### Context

`gh aw add` compiles the added workflow and any local `.md` dependencies it pulls in: `dispatch-workflow` targets and `call-workflow` workers. Each compile ends with `ensureGitAttributes()`, which adds the `linguist-generated` entry for `.lock.yml` files to `.gitattributes`. The add step already honors `--no-gitattributes`, but the compile steps called `ensureGitAttributes()` unconditionally, so `.gitattributes` could still be created or modified when the flag was set. The integration test comment acknowledged this ("The compile step may still create .gitattributes"). The existing unit tests logged errors instead of failing and did not exercise dependencies, so the gap went unnoticed (see issue #76915).

### Decision

We will pass `NoGitattributes` through the per-call options structs (`trackedWorkflowCompileOptions` and `compileDepsOptions`) and skip `ensureGitAttributes()` when it is set, for the top-level workflow and its dependencies alike. The flag then means "the add command never touches `.gitattributes`," not "the add step skips it." The primary driver is correctness of an explicit user opt-out. The new code paths take options structs rather than longer positional parameter lists, following [ADR-52982](52982-options-struct-for-parameter-heavy-functions.md).

### Alternatives Considered

#### Alternative 1: Add a `noGitattributes bool` parameter to each compile helper

This is the smallest diff and matches the existing style. We did not choose it because `compileWorkflowWithTrackingAndRefreshAndActionRef` already takes eight parameters, and every call site would change again for the next flag. This was a close call on diff size, but the options-struct approach is the pattern already recorded in ADR-52982.

#### Alternative 2: Snapshot and restore `.gitattributes` around dependency compilation

We could let compilation run unchanged and then restore the file's previous contents. We rejected this because it writes and then reverts a file that should not be touched, it must also undo file-tracker bookkeeping, and an interruption can leave a modified file behind. A flag check avoids the write entirely.

### Consequences

#### Positive
- `--no-gitattributes` now prevents `.gitattributes` writes in the top-level workflow compile and in dispatch and call-workflow dependency compiles on the tracked path.
- Adding a future option to the compile helpers needs one struct field, not a new parameter at every call site.
- Test assertions use `require`, so a failing `addWorkflows` call fails the test instead of being logged. Default-mode append, preservation, idempotency, and legacy-entry migration are now covered.

#### Negative
- **Latent gap on the untracked path.** `compileWorkflowWithRefreshAndActionRef` (`pkg/cli/add_workflow_compilation.go`) still calls `ensureGitAttributes()` unconditionally. It is reached through `compileWorkflowWithActionRef` when no `FileTracker` is passed: the `compileAddedWorkflow` fallback (`pkg/cli/add_workflow_content.go`) and the nil-tracker branch of `compileSafeOutputsWorkflowDependencies`. The add command always creates a tracker (`pkg/cli/add_command_orchestrator.go`, `pkg/cli/add_workflow_pr.go`), so this path is not reached in production today, but a future caller could bypass the opt-out.
- The new unit cases compile a workflow with no dispatch or call-workflow dependencies, so the dependency-path change has no direct test in this PR. [TODO: verify whether the integration tests cover it.]
- The two wrappers `compileDispatchWorkflowDependenciesWithActionRef` and `compileCallWorkflowDependenciesWithActionRef` have no production callers after this change. They remain only because `add_command_test.go` still calls them, which adds indirection.
- The tests use `t.Chdir`, which requires Go 1.24 or later, and cannot run in parallel.

#### Neutral
- Default behavior (no flag) is unchanged: every add still appends the lock-file entry.
- `compileWorkflowWithTrackingAndActionRef` keeps its signature as a thin adapter over the options struct.
- A `.gitattributes` write failure is still non-fatal, so the add does not fail because of it.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
