# ADR-58112: Warn on Missing Samples Replay Coverage

**Date**: 2026-09-03
**Status**: Draft
**Deciders**: dsyme, adr-writer agent

---

### Context

This pull request changes how `gh aw` behaves when workflows are compiled with `--use-samples` or `features.samples: true`. The PR evidence shows that a workflow could enable a non-builtin safe output, omit `samples:`, and still compile to an empty `GH_AW_SAMPLES` payload, causing the replay driver to perform nothing while the run still reported success. The diff fixes a concrete instance of that failure for `replace-label` and adds compiler diagnostics plus regression tests around the silent no-op behavior. Because this changes the contract of samples replay and establishes how missing sample coverage should be handled across safe outputs, the decision should be recorded explicitly.

### Decision

We will keep samples replay permissive but explicitly warn whenever an enabled, non-builtin safe output has no `samples:` entries configured. We will also require representative samples in workflows that are meant to be exercised under deterministic replay, and we will preserve runtime expressions in emitted samples while validating them at compile time. We chose warnings instead of compile errors because the PR states there is already expected behavior around compiling workflows with empty sample payloads, but silent success without exercising safe outputs is still important to surface.

### Alternatives Considered

#### Alternative 1: Fail compilation when any enabled safe output has no samples

This would make samples coverage mandatory under `--use-samples` and prevent workflows from compiling unless every enabled safe output declared at least one sample. It was considered because it would eliminate the silent-success failure mode completely. It was not chosen because the PR description and tests explicitly preserve existing workflows that intentionally compile with `GH_AW_SAMPLES: []`, so turning the condition into a hard error would be a breaking behavioral change.

#### Alternative 2: Keep current behavior and rely on tests or lock-file inspection

This would leave compilation unchanged and expect authors to detect missing replay coverage by checking fixture state, reviewing `GH_AW_SAMPLES`, or debugging replay artifacts manually. It was considered because it requires no new compiler logic or user-facing messaging. It was not chosen because the PR evidence shows this failure mode is easy to miss: runs can succeed with zero safe-output messages, making sampled end-to-end coverage appear valid when the target operation was never exercised.

### Consequences

#### Positive
- Authors get an explicit compiler warning when deterministic replay will skip configured safe outputs due to missing samples.
- Sampled end-to-end workflows become more trustworthy because representative safe-output samples are added and validated.
- Regression coverage now protects preservation of `${{ ... }}` expressions while still ensuring samples are collected into `GH_AW_SAMPLES`.

#### Negative
- Compiler output becomes noisier for workflows that intentionally use samples replay without defining samples for all enabled safe outputs.
- The codebase adds new helper and warning paths that must stay aligned with safe-output handler definitions.
- Warning-only enforcement still allows under-covered workflows to compile, so teams must still pay attention to warnings.

#### Neutral
- Documentation now needs to explain that samples replay coverage is advisory but visible.
- The decision distinguishes builtin versus non-builtin safe outputs in replay diagnostics.
- Existing workflows may continue to behave the same at runtime except for new warning text during compilation.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
