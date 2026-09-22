# ADR-62708: Add warning mode for unknown daily AI Credits

**Date**: 2026-09-22
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `max-daily-ai-credits` frontmatter guardrail currently fails activation when gh-aw cannot determine a complete 24-hour AI Credits total for the triggering user. The PR description explains that this blocks workflow activation in cases where accounting is unknown, even when users may prefer to continue while still surfacing the guardrail state. The implementation in this PR changes frontmatter schema, compiler output, documentation, and tests for the daily AI Credits guardrail. The architectural question is whether unknown AI Credits accounting should remain a hard stop in all cases or whether workflows may explicitly opt into warning-only behavior.

### Decision

We will extend the object form of `max-daily-ai-credits` with an optional `continue-on-error: true` flag that makes unknown daily AI Credits accounting warning-only for that guardrail step. gh-aw will preserve fail-closed behavior by default and emit `continue-on-error: true` in the compiled activation guardrail step only when the workflow author explicitly opts in. We chose this approach because it adds operational flexibility for incomplete accounting scenarios without weakening the existing default safety posture for workflows that rely on strict enforcement.

### Alternatives Considered

#### Alternative 1: Keep unknown accounting as an unconditional hard failure

This was the existing behavior and remains the simplest policy because no new frontmatter or compiler logic is required. It was not chosen because the PR evidence shows a concrete need to let some workflows continue when the total cannot be determined, while still reporting the condition to downstream consumers.

#### Alternative 2: Change the default behavior to warning-only for all workflows

This was a realistic alternative because it would eliminate activation failures caused by unknown accounting without requiring new configuration. It was not chosen because it would weaken the current fail-closed guardrail semantics for every workflow, including users who depend on strict enforcement of daily AI Credits limits.

### Consequences

#### Positive
- Workflow authors can explicitly choose warning-only handling when daily AI Credits totals are unknown.
- Existing workflows keep the current fail-closed default unless they opt into the new behavior.
- Schema, compiler, documentation, and regression tests now describe and enforce the same configuration contract.

#### Negative
- The guardrail configuration becomes more complex because authors must understand the difference between strict and warning-only behavior.
- Some workflows may continue despite incomplete AI Credits accounting, which can reduce enforcement strength when the option is enabled.

#### Neutral
- The change adds a new boolean field to the object form of `max-daily-ai-credits` but does not alter the scalar or disabled forms.
- Compiled workflows only differ when the new option is set, by adding `continue-on-error: true` to the guardrail step.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
