# ADR-62489: Propagate threat-detection cache miss guardrail

**Date**: 2026-09-22
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how gh-aw builds the threat-detection engine configuration for both inline and external detector paths. The PR description explains that the detection job was rebuilding `EngineConfig` from a fixed field list that omitted `MaxTurnCacheMisses`, so detection always fell back to the compile-time default of `5` even when the workflow raised the top-level `max-turn-cache-misses` setting. Under BYOK providers that only report `cached_tokens` intermittently, that mismatch causes the AWF proxy to fail closed with `max_cache_misses_exceeded` while the main agent job is allowed to continue with a higher configured limit. The implementation question is whether the workflow-level cache-miss guardrail should also govern the threat-detection job.

### Decision

We will inherit the workflow’s top-level `max-turn-cache-misses` value into the threat-detection engine configuration whenever detection does not set its own value. gh-aw will apply that inheritance in both the inline detection execution path and the external detector workflow data path, while continuing to leave `max-turns` independent for detection. We chose this because the cache-miss setting describes provider behavior for the shared LLM/API proxy rather than a per-job budget, so detection should enforce the same guardrail as the primary agent job.

### Alternatives Considered

#### Alternative 1: Keep the detection job on the compile-time default

This was the pre-change behavior and was effectively the simplest option because no additional inheritance logic was needed. It was not chosen because the PR evidence shows that it causes detection to silently diverge from the workflow’s configured provider guardrail and fail closed on providers that intermittently report cache hits.

#### Alternative 2: Inherit all top-level execution limits symmetrically, including `max-turns`

This was considered because symmetric inheritance could make detection configuration feel more uniform. It was not chosen because the PR description and tests explicitly distinguish cache-miss handling from execution budgets: `max-turns` is a cap on work that could starve detection, while `max-turn-cache-misses` is a provider-facing guardrail that should remain consistent across jobs.

### Consequences

#### Positive
- Threat-detection jobs now respect the same cache-miss tolerance as the main workflow when both use the same LLM provider.
- Workflows that raise `max-turn-cache-misses` no longer have detection silently revert to `5` and fail closed unexpectedly.
- Regression tests now cover inline detection inheritance, external detector inheritance, default fallback, and the intentional non-inheritance of `max-turns`.

#### Negative
- The compiler now carries explicit inheritance logic for another engine setting, which increases configuration coupling between the main workflow and detection paths.
- Future engine-config fields will require deliberate decisions about whether they are provider guardrails or per-job budgets, rather than assuming one inheritance rule fits all fields.
- Readers must understand why `max-turn-cache-misses` inherits while `max-turns` does not, which adds nuance to the configuration model.

#### Neutral
- The change updates frontmatter documentation to state that `max-turn-cache-misses` also applies to threat detection.
- Recompiled lock output changes only where a workflow actually sets the field, as shown by the `smoke-crush` lockfile update.
- Detection-specific overrides can still take precedence because inheritance only fills an unset or non-positive detection value.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
