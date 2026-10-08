# ADR-66928: Scope Experiment Expressions to Their Evaluating Job and Keep Security Reminders Advisory

**Date**: 2026-10-09
**Status**: Draft
**Deciders**: pelikhan [TODO: verify full decider list]

---

### Context

Experiment variants are declared in gh-aw frontmatter as `${{ experiments.<name> }}` and compiled into real GitHub Actions expressions. For jobs downstream of activation, the compiler rewrites them to `needs.activation.outputs.<name>` (`RewriteExperimentsReferenceForDownstreamJobs`). The agentic run-info step (`generate_aw_info` in `pkg/workflow/compiler_yaml_step_lifecycle.go`) however runs *inside* the activation job, where `needs.activation` does not exist and the gh-aw-only `experiments.*` context is not a valid Actions context. Three Copilot smoke workflows (`smoke-copilot`, `smoke-copilot-aoai-apikey`, `smoke-copilot-aoai-entra`) were therefore rejected by GitHub before any job started. Separately, the Entra smoke workflow intentionally grants `id-token: write`; the compiler's OIDC audience/trust-policy reminder was counted as a warning, so it failed strict dry-runs that treat warnings as errors even though nothing was wrong.

### Decision

We will scope experiment expressions to the job that actually evaluates them, and we will classify purely advisory security guidance as `info` rather than as a warning. Concretely, metadata emitted inside the activation job rewrites `needs.activation.outputs.<name>` back to `steps.pick-experiment.outputs.<name>` via `RewriteActivationOutputsToLocalStepOutputs`, while runtime model declarations and model-policy expansion keep the downstream form unchanged. The `id-token: write` reminder in `pkg/workflow/permissions_compiler_validator.go` is emitted with severity `info` and no longer increments the compiler warning count; genuine faults — invalid permission values and OIDC usage *without* the required `id-token: write` permission — remain hard errors. The primary driver is correctness of generated YAML plus signal quality: diagnostics should only fail a build when the workflow is actually wrong.

### Alternatives Considered

#### Alternative 1: Omit the experiment model from run-info metadata inside activation

The simplest fix for the invalid expression is to not emit `GH_AW_INFO_MODEL` at all (or emit a literal placeholder) when the model references an experiment variant. This was a close call because it removes the broken expression with no new rewriting logic. It was rejected because run details would then show `(none)` or a stale value for exactly the runs where the model is most interesting — experiment runs — losing the attribution needed to interpret experiment results.

#### Alternative 2: Promote the experiment value to a job-level env var or output and reference it uniformly

Instead of a context-aware rewrite, the compiler could publish the picked variant once into a single reference form (for example a job-level `env`) usable both inside and outside activation. This was considered because it collapses two reference shapes into one. It was rejected because job-level `env` cannot be computed from a step output within the same job, and introducing a third reference form would complicate the existing, already-tested downstream rewrite path and every consumer of `needs.activation.outputs.*`.

#### Alternative 3: Keep the OIDC reminder as a warning and suppress it per workflow

The reminder could stay a warning, with an opt-out (e.g. a frontmatter suppression key) applied to the Entra smoke workflow. This preserves the louder signal for workflows that grant OIDC carelessly. It was rejected because a correctly configured workflow would still need an explicit suppression, which normalizes suppression annotations and makes the real warning channel noisier over time; severity, not suppression, is the right lever for unconditional advice.

### Consequences

#### Positive

- The three Copilot smoke workflows compile to valid Actions YAML and are no longer rejected before any job starts; native actionlint no longer reports invalid experiment expressions.
- Run-info metadata keeps reporting the actually selected experiment model, preserving experiment attribution.
- Warnings-as-errors builds are no longer failed by advisory OIDC guidance, so remaining warnings carry real signal.
- Error coverage for misconfigured OIDC (missing `id-token: write`, invalid permission values) is unchanged.

#### Negative

- Two reference shapes for the same experiment value now exist (`needs.activation.outputs.*` and `steps.pick-experiment.outputs.*`), so any new code emitting expressions inside the activation job must remember to apply the local rewrite or it will regress in the same way.
- The OIDC reminder is now easier to miss in long compiler output, slightly reducing the chance an author reviews audience and trust-policy configuration.
- Correctness of the rewrite depends on the `pick-experiment` step ID remaining stable; renaming it would silently break generated metadata.

#### Neutral

- Three `.lock.yml` smoke workflows were regenerated; all 330 workflow locks remain synchronized (`make check-workflow-drift`).
- Compiler threat-detection specification 1.0.44, the CTR-001 mappings, and the changelog were updated alongside the behavior change.
- New unit coverage was added for inline/imported agents, compound expressions, diagnostic severity, and development-gate behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
