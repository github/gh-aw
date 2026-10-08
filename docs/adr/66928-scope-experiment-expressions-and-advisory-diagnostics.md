# ADR-66928: Scope Experiment Expressions to Their Evaluating Job and Keep Security Reminders Advisory

**Date**: 2026-10-09
**Status**: Proposed (implemented in PR #66928; maintainer acceptance pending)
**Decision owner**: pelikhan, the PR author. Maintainer approval is not implied by this record.

---

### Context

Experiment variants are declared in gh-aw frontmatter and referenced with `${{ experiments.<name> }}`. For jobs downstream of activation, the compiler rewrites these references to `needs.activation.outputs.<name>` (`RewriteExperimentsReferenceForDownstreamJobs`). The run-info step (`generate_aw_info` in `pkg/workflow/compiler_yaml_step_lifecycle.go`) runs inside activation, where neither `needs.activation` nor the gh-aw-only `experiments.*` context is available. Three Copilot smoke workflows (`smoke-copilot`, `smoke-copilot-aoai-apikey`, `smoke-copilot-aoai-entra`) were rejected before any job started.

Experimental sub-agent declarations also need concrete patterns for audit matching and model-routing request admission. Expanding the raw expression as a model yields no patterns, even when every variant is a known alias. Sub-agent sections are stripped before ordinary prompt-expression extraction; accepting compound model expressions based only on metadata rewriting would promise runtime evaluation that is not supported.

Separately, the Entra smoke workflow intentionally grants `id-token: write`. The unconditional OIDC audience/trust-policy reminder was counted as a warning, preventing strict compilation even though granting the permission is not itself a configuration fault. Whether the external audience, trust policy, and cloud roles are appropriate remains the workflow owner's responsibility.

### Decision

Scope experiment expressions to the job that evaluates them. Activation metadata first rewrites declared experiment references to downstream outputs, then converts them to `steps.pick-experiment.outputs.<name>` through `RewriteActivationOutputsToLocalStepOutputs`. Runtime sub-agent declarations retain their authored direct experiment reference so the existing runtime substitution selects the variant.

Expand every declared variant through `ModelMappings`, normalize its provider, and deduplicate the resulting patterns through one helper shared by metadata and model-routing admission. Existing `models.allowed` and `models.blocked` checks still apply; separately admitted sub-agent models do not become router candidates. The union describes possible models, not proof that a particular selected variant was honored or that delegation occurred.

Reject compound experiment-backed sub-agent models, including fallback and function expressions, before generating YAML. The error directs authors to use `${{ experiments.<name> }}` and declare complete models or aliases as variants. Reject undeclared experiment references as well.

Emit the unconditional `id-token: write` reminder as `info` without incrementing the warning count. Preserve its audience/trust-policy guidance, mandatory OIDC permission checks, and rejection of invalid permission values. This changes diagnostic severity, not workflow permissions or external federation settings. Compiler threat-detection specification 1.0.44 records the same distinction.

### Alternatives Considered

#### Alternative 1: Omit the experiment model from run-info metadata inside activation

Omitting `GH_AW_INFO_SUB_AGENT_MODELS` or recording a literal placeholder avoids the invalid expression, but loses the selected-model attribution needed to interpret experiment results. Context-aware rewriting preserves that information.

#### Alternative 2: Promote the experiment value to a job-level env var or output and reference it uniformly

A job-level `env` cannot depend on a step output produced within that job. A single uniform reference therefore does not replace the existing local-step and downstream-job forms; introducing another indirection would add consumers without removing the context distinction.

#### Alternative 3: Keep the OIDC reminder as a warning and suppress it per workflow

A suppression key would require correctly configured workflows to opt out of an unconditional reminder. Classifying the message as information avoids normalizing suppressions while preserving warnings and errors for actual faults.

#### Alternative 4: Evaluate arbitrary compound sub-agent model expressions

This would require coordinated changes to runtime expression evaluation and finite model-policy expansion, not just a metadata rewrite. Rejecting these unsupported forms is narrower and avoids silently selecting a fallback or admitting an unresolvable alias. Full expression support remains a separate feature.

### Consequences

#### Positive

- The three smoke workflows emit valid Actions contexts for experimental sub-agent metadata. Unrelated workflow-validation findings are not addressed by this decision.
- Run-info metadata keeps reporting the actually selected experiment model, preserving experiment attribution.
- Audit matching and request admission recognize the concrete models behind experiment aliases.
- Warnings-as-errors builds are no longer failed by advisory OIDC guidance, so remaining warnings carry real signal.
- Error coverage for misconfigured OIDC (missing `id-token: write`, invalid permission values) is unchanged.

#### Negative

- Two reference shapes for the same experiment value now exist (`needs.activation.outputs.*` and `steps.pick-experiment.outputs.*`), so any new code emitting expressions inside the activation job must remember to apply the local rewrite or it will regress in the same way.
- The OIDC reminder is now easier to miss in long compiler output, slightly reducing the chance an author reviews audience and trust-policy configuration.
- Correctness of the rewrite depends on the `pick-experiment` step ID remaining stable; renaming it would silently break generated metadata.
- Compound experiment-backed sub-agent models now fail compilation instead of appearing supported. Authors must move complete model values into their variants.
- Union patterns can match an observed non-selected variant. They establish model presence only, not selected-model correctness or invocation.

#### Neutral

- Generated smoke locks are regenerated with the compiler; `make check-workflow-drift` checks synchronization.
- Compiler threat-detection specification 1.0.44, the CTR-001 mappings, and the changelog were updated alongside the behavior change.
- Regression coverage includes inline/imported agents, rejection of compound model expressions, alias expansion, routing policy, audit matching, diagnostic severity, and development-gate behavior.

---

Implementation: [PR #66928](https://github.com/github/gh-aw/pull/66928).
