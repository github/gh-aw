# ADR-63490: Fallback safe outputs GitHub token

**Date**: 2026-09-25
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR fixes a failure in compiled `safe_outputs` jobs when `safe-outputs.staged: true` is combined with a top-level `safe-outputs.github-app`. In that configuration, the workflow may skip emitting the `safe-outputs-app-token` minting step because no enabled handler consumes the global app permissions, but other compilation paths still emitted expressions that referenced that step directly. The PR description and diff show this caused `Process Safe Outputs` to fail with `Input required and not supplied: github-token`, especially for fully staged or Linear-only safe-output configurations. The decision is how the compiler should determine when the global app token step exists and how token expressions should behave when it does not.

### Decision

We will centralize the predicate that decides whether the global `safe-outputs-app-token` step is minted and reuse that predicate everywhere the compiler emits or resolves safe-output GitHub tokens. When the minting step may be absent, generated token expressions will fall back to the regular safe-output token instead of referencing the app-token step alone. We chose this because the diff shows the bug came from two code paths drifting apart on the same architectural condition, so a shared predicate plus explicit fallback is the simplest way to keep compiled workflows valid across staged and non-staged configurations.

### Alternatives Considered

#### Alternative 1: Always mint the global GitHub App token step when `safe-outputs.github-app` is configured

This was a realistic option because it would make every `steps.safe-outputs-app-token.outputs.token` reference valid and avoid conditional fallback logic. It was not chosen because the PR description and code comments indicate some configurations intentionally skip the minting step when no enabled handler consumes the global app, including Linear-only setups and staged handlers, so always minting would weaken credential separation and expand unnecessary token issuance.

#### Alternative 2: Keep the existing minting behavior and patch each caller independently

This was viable because the broken expressions appeared in a small number of token resolution paths. It was not chosen because the PR evidence shows the root problem was duplicated logic between emission and resolution paths; patching callers independently would preserve that drift risk and make future safe-output token changes easier to break again.

### Consequences

#### Positive
- Compiled workflows no longer emit dangling `steps.safe-outputs-app-token.outputs.token` references when the minting step is intentionally skipped.
- A single shared predicate reduces the chance that future compiler paths disagree about whether the global app token step exists.
- Regression tests now cover both staged and non-staged configurations, improving confidence in safe-output token generation.

#### Negative
- The compiler gains another shared helper whose semantics must remain synchronized with safe-output permission computation.
- Token resolution logic becomes slightly more complex because generated expressions may now combine app-token and fallback-token sources.
- Future maintainers must preserve the distinction between handlers that may safely fall back and handlers that must never use the default Actions token.

#### Neutral
- Non-staged configurations that already mint the app token keep the existing direct expression behavior.
- The change affects workflow compilation behavior and tests rather than introducing a new user-facing workflow feature.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
