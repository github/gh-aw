# ADR-77058: Engine-Default Durable Continuation

**Date**: 2026-11-12
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Behavior-defined engines can declare artifact-backed session and checkpoint continuation under `behaviors.durable`, as introduced in ADR-75097. Until now, durable execution ran only when each consuming workflow also set `engine.durable` in its own frontmatter, because `mergeImportedEngineConfig` lets a main workflow's `engine: { id: <name> }` selection replace imported engine settings. Engines such as Atomic expect session and workflow run state to persist on every eligible run without requiring every workflow author to repeat `engine.durable`. At the same time, an engine-level default must allow per-workflow opt-out and must never break existing valid workflows that use triggers or concurrency settings incompatible with durable checkpoint serialization.

### Decision

We will add an optional integer field `behaviors.durable.default-retention-days` (`1` to `90`) on behavior-defined engine definitions, and allow `engine.durable: false` on workflow engine configurations to opt out. During `setupWorkflowBuildContext`, when the resolved engine definition sets `default-retention-days` and the workflow neither sets `engine.durable` nor disables it with `false`, the compiler populates `WorkflowData.EngineConfig.Durable` once so all downstream concurrency, artifact, permission, and execution paths reuse the existing `Durable != nil` contract.

When durability comes only from `default-retention-days` and the workflow uses `workflow_call`, `tools.work-queue`, explicit workflow or engine `concurrency`, or a custom `engine.command`, `engine.driver`, or `engine.harness-script`, the compiler leaves `Durable` unset and emits a warning naming why durable mode was skipped. Workflows that explicitly configure `engine.durable` keep the existing compile errors for those cases. The experimental-feature warning remains active and states when `engine.durable` came from the engine default.

### Alternatives Considered

#### Alternative 1: Merge imported top-level `engine.durable` across `engine: { id: <name> }`

The compiler could copy `engine.durable` from an imported file even when the main workflow selects `engine: { id: <name> }`. We did not choose this because engine definitions put runtime behavior under `behaviors`, whereas top-level `engine` fields in shared workflows represent workflow configuration overrides. Mixing the two would blur the boundary between declarative engine capabilities and workflow-level settings, and would still require an explicit opt-out and incompatibility-skip policy.

#### Alternative 2: Require every consuming workflow to set `engine.durable`

Keep durable continuation strictly opt-in per workflow and document `engine.durable` in each engine's setup guide. We did not choose this because workflows that omit the field silently lose session and workflow-journal persistence across runs, defeating engines designed around resumable state by default.

#### Alternative 3: Fail compilation when an engine default meets an incompatible workflow shape

Treat `default-retention-days` identically to explicit `engine.durable` during validation, rejecting `workflow_call`, `work-queue`, or explicit `concurrency` workflows unless the author adds `engine.durable: false`. We did not choose this because turning on a default in a shared engine definition would break existing valid workflows on upgrade. Skipping implicit durability with a clear warning preserves backward compatibility while keeping explicit `engine.durable` errors strict.

### Consequences

#### Positive

- Behavior-defined engines can make durable session continuation their default in one declaration without duplicating `engine.durable` across consuming workflows.
- Resolving the default into `WorkflowData.EngineConfig.Durable` during setup reuses every existing durable compilation path without scattering new conditionals.
- Existing `workflow_call`, `work-queue`, custom-concurrency, and custom-command workflows continue to compile without error when an imported engine adopts `default-retention-days`.

#### Negative

- Workflows importing an engine that sets `default-retention-days` automatically upload and restore checkpoint artifacts and switch to serialized `gh-aw-durable-<workflow>` concurrency unless they set `engine.durable: false`.
- Artifact checkpoints can contain sensitive session history visible to users with repository artifact read access; enabling durability in an engine definition applies that exposure to every eligible workflow using the engine.

#### Neutral

- Built-in engines and existing shared engine definitions omit `default-retention-days`, so no existing workflow or compiled `.lock.yml` file changes.
- Explicit `engine.durable` retention values override `default-retention-days`, and explicit `engine.durable` incompatibilities remain compile errors.
