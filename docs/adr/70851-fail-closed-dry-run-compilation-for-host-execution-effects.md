# ADR-70851: Fail Closed in Dry-Run Compilation for Host Execution Effects

**Date**: 2026-10-21
**Status**: Accepted
**Deciders**: pelikhan, Copilot (PR #70851)
**Amended**: 2026-11-08 (PR #75978): accept `steps` and `post-steps` only with their code and credentials omitted from dry-run locks.

---

## Context

`gh aw compile --dry-run` emits diagnostic lock files with compiler-managed mutations staged rather than applied; compilation does not execute the workflow. Staging built-in safe outputs alone does not neutralize authored host steps, custom jobs, reusable workflow calls, external MCP servers, custom scripts, Bash graders, or cloud credentials. Overrides on built-in job names can also introduce executable code or secrets outside the staged handlers.

A hosted diagnostic run therefore needs a compile-time admission policy for these effects, not just staged safe outputs. The compiler cannot establish that arbitrary authored code or external services are read-only. Separately, accepting scanner findings merely because they already occur in a baseline confuses regression tracking with admission: a known actionable finding remains actionable.

## Decision

Apply a fail-closed policy to dry-run compilation while leaving ordinary compilation unchanged.

### Reject effects that cannot be represented safely

Reject authored `pre-steps`, `pre-agent-steps`, and `on.steps`; custom MCP servers and `mcp-scripts`; `secret-masking.steps`; `safe-outputs.scripts`, `safe-outputs.steps`, and `safe-outputs.actions`; threat-detection `steps` and `post-steps`; and enabled Bash `graders.<id>.run` declarations. Reject authored `id-token: write` permissions at workflow, activation, and safe-output scope. Built-in job augmentation permits only `needs`, `if`, `timeout-minutes`, and `continue-on-error`; all other non-null fields are rejected, including execution, runner, environment, permission, and secret overrides.

Validation checks resolved workflow data, including imported declarations that participate in merging. It does not expand import semantics to activate previously ignored fields.

These declarations alter setup, tools, or host execution in ways that cannot be faithfully staged. Compilation reports the unsupported declarations instead of silently removing them. Diagnostic workflows must remove them and any imports that supply them.

### Omit ordinary authored workflow steps

Accept workflow `steps` and `post-steps`, including resolved imports, but remove their code and credentials before constructing jobs or wiring token actions. These steps are absent from emitted dry-run locks and cannot execute when those locks are separately dispatched. CLI and JSON reports identify this disabled surface explicitly. Normal compilation retains the original steps.

This amendment permits compile-only validation without admitting host effects. It is not a simulation of setup or cleanup: missing prerequisites and cleanup reduce live-test coverage, and the agent and compiler-owned steps still execute on authorized dispatch.

### Preserve job topology with disabled placeholders

Replace custom jobs, custom safe-output jobs, and compiler-managed reusable workflow calls with disabled regular jobs. Each placeholder uses `if: ${{ !always() }}`, `permissions: {}`, a static policy-compatible runner, and an inert echo step. It retains job identity, dependencies, and declared or referenced output names, with output expressions replaced by empty strings. It does not retain authored steps, reusable targets, inputs, secrets, environments, or matrix-dependent runner configuration.

Disabling rather than deleting preserves dependency and output references in the emitted graph. The placeholder is not a simulation of the original job: skipped jobs do not execute or produce real outputs, and downstream behavior can differ.

### Separate scanner identity from admission

Fail the dry-run scanner gate on every medium-or-higher or unclassified finding, whether existing or added. Low and informational findings remain non-blocking. Retain baseline, existing, added, and ignored counts to explain changes, but do not treat baseline membership as a waiver. Scanner execution, extraction, and parsing failures remain blocking rather than being interpreted as clean results.

Canonicalize workflow file identities, including symlink resolution, and use stable script locations to avoid reporting unchanged findings as additions after generated lines move. Trigger normalization operates on YAML nodes so existing comments, including zizmor suppressions, survive adding `workflow_dispatch`; the admission policy does not require discarding authored scanner annotations.

## Alternatives Considered

### Stage built-in safe outputs only

This preserves compatibility and maximizes execution coverage, but leaves unrelated host effects and built-in job overrides active. Documentation alone cannot provide the fail-closed admission needed for unattended diagnostics.

### Sandbox or mock every authored effect

This could preserve more workflow behavior, but would require isolation and service emulation beyond a compiler rewrite. Arbitrary scripts, external MCP effects, and cloud credentials cannot be reliably intercepted by staging safe outputs. No general execution sandbox is claimed.

### Silently strip unsupported declarations or delete jobs

Silently removing setup or tools makes a diagnostic workflow misleading and hides unverified behavior from authors. Deleting jobs also breaks dependency and output references. Explicit rejection is preferable for unsupported declarations; disabled placeholders are a narrower structural compromise for jobs. The amendment permits omitting ordinary `steps` and `post-steps` only with explicit reporting of the disabled surface and reduced coverage.

### Admit unchanged scanner findings

A differential-only gate reduces migration friction, but permits known actionable findings indefinitely. Differential counts remain useful diagnostics, not evidence that a finding is safe.

## Consequences

Dry-run compilation now rejects workflows that previously compiled with unsupported declarations. Authors must create reduced diagnostic workflows, and existing actionable findings must be resolved before admission. This is an intentional compatibility change to dry-run behavior, not a change to ordinary compilation.

Disabled jobs preserve graph references but reduce coverage: dependent jobs can skip, and empty or absent outputs do not exercise production data flow. A successful diagnostic run cannot validate the disabled jobs or reusable workflow bodies. The explicit policy and tests must also evolve when new execution surfaces are added.

A successful dry-run compile is only one precondition for hosted execution, not proof of a side-effect-free workflow. Agent shell execution, custom engines, supplied credentials, runner identity, and external services still require independent review. Dispatch must use the exact reviewed source and compiled lock revision under the [active debugging gates](../../.github/aw/debug-agentic-workflow.md); `--dry-run` compilation alone neither starts a run nor authorizes one.
