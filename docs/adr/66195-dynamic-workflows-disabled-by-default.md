# ADR-66195: Require Explicit Opt-In for Dynamic Workflows on Every Agentic Engine

**Date**: 2026-10-06
**Status**: Draft
**Deciders**: gh-aw maintainers (pending review; PR author: pelikhan)

---

### Context

gh-aw compiles workflow markdown into GitHub Actions jobs and can expose a "dynamic workflows" capability, which lets an agentic engine register and invoke nested workflows (and structured subagents) at runtime. Until this change, `EngineConfig.DynamicWorkflowsEnabled()` returned `true` whenever the engine setting was omitted (`e == nil || e.DynamicWorkflows == nil || *e.DynamicWorkflows`), so every compiled workflow on every engine silently carried the feature's CLI flags, tool permissions, SDK capabilities, and activation/restoration behavior. PR #66195 added an end-to-end smoke workflow for the Copilot CLI dynamic-workflow path and, in doing so, established that native invocation is still unverified: on Copilot CLI 1.0.92 with `--experimental`, the extension loads and the workflow registers, but no workflow-runner tool is exposed to the agent, and CLI 1.0.92 additionally gates workflow tool availability behind token-based-billing or trusted HMAC authentication. Shipping an unverified, runtime-gated capability as the default affects all 322 compiled workflows in this repository and every downstream consumer.

### Decision

We will make dynamic workflows **opt-in** for every agentic engine: `DynamicWorkflowsEnabled()` now returns `e != nil && e.DynamicWorkflows != nil && *e.DynamicWorkflows`, so only an explicit `engine.dynamic-workflows: true` activates the feature, and then only if the engine's capability metadata supports it. We will additionally classify the Copilot variant as experimental, emitting `Using experimental feature: copilot.dynamic-workflows` from `compiler_validators.go` when a Copilot workflow opts in. The primary driver is risk containment: the capability is not verifiable on current runners, so the default must not expose it, while the shared helper keeps native CLI settings, tool permissions, SDK capabilities, activation snapshots, and trusted-configuration restoration consistent with a single source of truth.

### Alternatives Considered

#### Alternative 1: Keep the enabled-by-default behavior and gate only at runtime

The engine could continue to emit the dynamic-workflow flags and let the Copilot CLI's own tool-availability and authentication gate decide whether the capability materializes. This was considered because it requires no compiler change and preserves the capability for the environments where it does work. It was rejected because the generated lock files would keep carrying `--allow-tool workflow` and elevated SDK capabilities for workflows that never asked for them, and because the runner evidence in this PR shows the runtime gate produces an opaque failure rather than a clean degradation.

#### Alternative 2: Disable the feature only for the Copilot engine

Since the observed failure is specific to the Copilot CLI's tool catalog (see `copilot-agent-runtime#21034`), the default could have been flipped for Copilot alone while leaving Claude and other engines enabled. This was a genuine close call and would have been a smaller blast radius. It was rejected because `DynamicWorkflowsEnabled()` is a single shared helper consumed by CLI settings, tool permissions, SDK capability construction, activation snapshots, and trusted-config restoration; per-engine defaults would fork that logic into engine-specific branches and make the activation/restoration contract harder to reason about. Engine capability metadata still provides per-engine refusal on top of the shared default.

#### Alternative 3: Remove the dynamic-workflow code path entirely until it is verified

Deleting the feature would guarantee no accidental exposure. It was rejected because the smoke workflow added in this PR exists precisely to detect when the runtime gate lifts, and that smoke requires the opted-in code path to remain compilable and executable.

### Consequences

#### Positive

- No workflow emits dynamic-workflow CLI flags, tool permissions, or SDK capabilities unless its author explicitly asked for them, shrinking the default agent tool surface.
- The experimental warning gives authors an explicit signal that `copilot.dynamic-workflows` is unverified before they depend on it.
- A single shared helper keeps the default consistent across CLI settings, permissions, SDK capabilities, activation, and restoration, so there is one place to flip the default once the capability is verified.
- The retained smoke workflow (explicitly opted in) continues to report the real FAIL rather than a fabricated pass, so the gate lifting will be detected.

#### Negative

- This is a breaking behavior change for any existing workflow that relied on the implicit default; those workflows must now add `engine.dynamic-workflows: true`.
- Every affected `.lock.yml` had to be regenerated, producing a large diff (266 files) that is mostly mechanical churn and will conflict with concurrent branches.
- The experimental warning adds compiler noise for the opted-in smoke workflow on every compile.
- Native dynamic-workflow invocation remains unverified; this ADR records a containment decision, not a resolution of the underlying runtime limitation.

#### Neutral

- Schema definitions (`main_workflow_schema.json`), engine documentation, SDK contract fixtures, and compiler golden outputs were updated together to reflect the new default; they must stay in sync if the default is revisited.
- PR-checkout restoration remains available independently of the dynamic-workflow default and is unaffected.
- The smoke workflow is pinned to Copilot CLI **1.0.92** with `--experimental` while the repository-wide CLI default is unchanged, so the pin must be revisited separately.
- Unrelated pre-existing lint findings (`compiler_validators.go`, `copilot_engine_execution.go`, `create_project.cjs:253`) and a Go 1.27 `goleak` HTTP/2 exclusion mismatch were deliberately left unfixed in this PR.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
