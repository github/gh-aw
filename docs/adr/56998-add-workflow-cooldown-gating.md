# ADR-56998: Add Workflow Cooldown Gating

**Date**: 2026-08-29
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request adds a new `on.cooldown` frontmatter field that changes how compiled workflows decide whether to activate the `agent` job. The implementation parses a literal Go duration, injects a pre-activation run-history check, grants `actions: read` when needed, and skips reruns when the most recent completed workflow run already started the `agent` job within the configured window. The PR also documents fail-open behavior when run history cannot be queried and excludes runs where the agent job was skipped. Because the PR adds more than 100 lines in business-logic directories and introduces a new workflow execution control, the decision should be recorded explicitly.

### Decision

We will support workflow-level cooldown gating through a new `on.cooldown` frontmatter field that compiles into a pre-activation run-history check against recent completed workflow runs. The cooldown must be a literal Go duration string of at least five minutes, and GitHub Actions expressions will be rejected so the compiler can validate behavior deterministically. The generated workflow will inspect prior runs for a started `agent` job, block execution when that run finished within the cooldown window, and fail open when run history is unavailable. We chose this approach because the PR evidence shows a need to reduce repeated agent executions while keeping configuration declarative and consistent with existing pre-activation gating features.

### Alternatives Considered

#### Alternative 1: Use Existing Rate Limiting or Schedule Controls Instead of a New Cooldown Field

Keep repeated-execution control within existing mechanisms such as rate limiting or cron schedules and avoid adding another `on` field.

This was considered because it would reduce feature surface area and reuse existing concepts. It was not chosen because the PR evidence shows a distinct need to gate execution based on the latest completed run that actually executed the `agent` job, which is more specific than static schedules or the existing rate-limit behavior.

#### Alternative 2: Allow Dynamic GitHub Actions Expressions for Cooldown Values

Permit `on.cooldown` to accept expressions so repositories can compute cooldown durations from inputs, vars, or other runtime state.

This was considered because it would make the feature more flexible for advanced workflows. It was not chosen because the PR explicitly validates only literal duration strings, rejects expressions, and relies on compile-time determinism for clear validation and predictable generated workflow behavior.

### Consequences

#### Positive
- Repositories can declaratively prevent redundant agent executions shortly after a recent completed run without changing the workflow body itself.
- The compiler keeps cooldown behavior aligned with other pre-activation checks by validating configuration early and generating a dedicated gating step.
- Ignoring runs where the `agent` job did not start avoids restarting the cooldown for skipped executions.

#### Negative
- The feature increases workflow compiler and setup-script complexity by adding run-history inspection, new constants, schema changes, and pre-activation conditions.
- Cooldown enforcement depends on GitHub Actions run-history APIs and therefore requires additional `actions: read` permission in generated workflows.
- Failing open when history is unavailable preserves availability but means cooldown protection is not guaranteed during API or permissions problems.

#### Neutral
- The cooldown is measured from the completion time of the most recent qualifying workflow run, regardless of whether that agent execution succeeded or failed.
- The implementation treats `on.cooldown` as another processed `on` field and comments it out in the compiled lockfile for documentation.
- Tests now cover both frontmatter parsing and generated workflow output for the new gating behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
