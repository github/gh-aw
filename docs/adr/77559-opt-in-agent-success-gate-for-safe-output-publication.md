# ADR-77559: Opt-In Agent-Success Gate for Safe-Output Publication

**Date**: 2026-11-14
**Status**: Draft
**Deciders**: Unknown (AI-drafted from PR #77559 and issue #77504; human deciders [TODO: verify])

---

### Context

Safe-output jobs (threat detection, built-in handlers, custom safe jobs, and asset upload) currently run whenever the agent job was not skipped or cancelled. This lets them process staged outputs after the agent job has failed, which is intentional so that failures can still be reported. As a result, a deterministic check (for example a failing `post-steps` step) that fails the agent job does not stop staged outputs from being published (issue #77504). Workflows that rely on such checks as a publication gate need a way to make agent success a precondition, without breaking existing workflows that depend on publication after failure. This PR adds 199 lines under `pkg/`, which exceeds the 100-line default ADR threshold.

### Decision

We will add a boolean `safe-outputs.require-agent-success` option, default `false`. When `true`, the compiler ANDs `needs.agent.result == 'success'` into the `if:` conditions of the detection job, the main `safe_outputs` job, custom safe jobs, and `upload_assets`. The conclusion and failure-reporting jobs stay unchanged. The value is a literal boolean only (not an expression). For imports, an explicit main-workflow value wins; otherwise any imported `true` enables the gate. The primary driver is backward compatibility: an opt-in flag closes the publication hole without changing the behavior of existing workflows.

### Alternatives Considered

#### Alternative 1: Change the default so safe outputs require agent success

This would fix the issue for all workflows with no new configuration. It was not chosen because it would silently change behavior for workflows that rely on safe outputs after agent failure, such as failure reporting, and would be a breaking change.

#### Alternative 2: Let `post-steps` delete or invalidate staged output files on failure

A deterministic check could remove the staged output files itself. This was not chosen because it leaves the safe-output restrictions on accessing those files from `post-steps` in place. It would also make every workflow author implement the gate by hand, and it would be easy to get wrong.

#### Alternative 3: Add a general-purpose gate hook or templatable expression

A user-supplied condition could control publication. This offers more flexibility, but it adds new surface area and expression-injection concerns. The PR restricts the setting to a literal boolean and adds no new hook.

### Consequences

#### Positive
- Workflows can make a deterministic check an enforceable publication gate with a single flag.
- The default of `false` preserves existing behavior, including failure-reporting flows.
- The gate is applied uniformly across detection, built-in handlers, custom safe jobs, and asset upload.

#### Negative
- Authors must know the check has to fail the agent job; `continue-on-error: true` steps do not block publication.
- With the flag on, safe outputs are not processed after an agent failure, so failure-time outputs are lost.
- The import merge rule (a main value overrides, otherwise any `true` wins) adds semantics that authors must learn. An imported `true` cannot be vetoed except by an explicit main-workflow `false`.

#### Neutral
- The `require-agent-success` key is registered as a safe-outputs meta field, not an output type, in the schema and `safeOutputMetaFields`.
- The conclusion job is unchanged, so failure reporting still works.
- Docs in `safe-outputs.md` describe the new option; no new gate hook is introduced.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
