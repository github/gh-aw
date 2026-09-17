# ADR-61599: Rewrite experiment model references

**Date**: 2026-09-17
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request fixes a workflow-compilation bug where `${{ experiments.model }}` was accepted in `engine.model` or the top-level `model:` field but was emitted unchanged into compiled lock files. The PR description shows that the resulting lock file set engine-related environment variables such as `ANTHROPIC_MODEL`, `GH_AW_INFO_MODEL`, and `GH_AW_ENGINE_MODEL` to the invalid GitHub Actions context `experiments.model`, which fails at runtime because `experiments` is not a valid workflow context. The diff adds rewrite helpers in the workflow compiler, applies them when extracting model configuration, and adjusts activation-job rendering so the same logical model reference uses the correct context in each job scope. The design question is how gh-aw should represent experiment-selected model values in compiled workflows when a model expression originates from frontmatter.

### Decision

We will rewrite declared `${{ experiments.<name> }}` references in model configuration to valid job-scoped expressions during workflow compilation. For jobs downstream of activation, gh-aw will emit `needs.activation.outputs.<name>`, and for the activation job's own info step it will emit `steps.pick-experiment.outputs.<name>`. We chose this because experiment-selected model values are already materialized by the activation job, and expressing them through existing step and job outputs fixes runtime validity without introducing a new workflow context model.

### Alternatives Considered

#### Alternative 1: Leave `experiments.<name>` unchanged in compiled workflow output

The existing behavior effectively treated the model expression as opaque text and passed it through to the lock file. This was considered because it keeps the compiler simpler and avoids special-case rewriting logic. It was not chosen because the PR evidence shows the resulting workflows are invalid at runtime: GitHub Actions rejects `experiments.model` as an unknown context.

#### Alternative 2: Introduce a new dedicated runtime context or bespoke environment handoff for experiment model values

Another option would be to invent a separate mechanism for carrying the chosen experiment variant into later jobs, rather than reusing activation outputs and step outputs. This was considered because it could isolate experiment interpolation from existing job-scope conventions. It was not chosen because the current workflow architecture already exposes experiment selections through activation outputs, so adding a parallel transport would increase complexity and duplicate existing data flow.

### Consequences

#### Positive
- Compiled workflows no longer emit invalid `experiments.<name>` expressions in engine model environment variables.
- The chosen experiment model is referenced using scope-correct GitHub Actions expressions in both downstream jobs and the activation job.
- Regression coverage now verifies helper rewrites, prefix-collision safety, and end-to-end compilation for both `engine.model` and top-level `model:` forms.

#### Negative
- The compiler now contains additional regex-based rewrite logic that must remain aligned with experiment-name parsing rules.
- Model handling becomes more context-sensitive because the activation job and downstream jobs require different rewritten expressions.
- Future changes to experiment output naming or job structure will need corresponding updates to the rewrite helpers and tests.

#### Neutral
- Only declared experiment names are rewritten; unrelated text and undeclared references are intentionally left unchanged.
- The change does not alter the external frontmatter syntax for configuring experiments or model selection.
- The implementation adds helper functions and tests but does not introduce a new user-facing configuration field.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
