# ADR-80402: Make Issue-Intent an Experimental Opt-In Feature

**Date**: 2026-11-23
**Status**: Draft
**Deciders**: Unknown (PR author: pelikhan)

## Context

Issue-intent metadata (`rationale`, `confidence`, `suggest`) lets an agent attach a justification and confidence level to label, assignment, issue-type, issue-field, and close actions. Under [ADR-46207](46207-default-on-issue-intent-metadata.md), this metadata was on by default for six safe-output handlers: `close-issue`, `add-labels`, `assign-to-agent`, `assign-to-user`, `set-issue-type`, and `set-issue-field`. Omitting `issue-intent` therefore routed these calls through the GraphQL intent path and added "INTENT ENCOURAGED" guidance to the generated tool schemas.

The feature is still experimental, but default-on behavior affected every workflow that used these outputs, including workflows that never opted in. The compiler did not flag it as experimental, so users received no signal that they were relying on it.

Forces:

- Experimental behavior should not apply silently to workflows that did not request it.
- Intent metadata is forwarded only through the GraphQL path. The legacy REST paths do not carry it, so the two paths must stay consistent.
- Workflows that depend on intent metadata without setting the flag will change behavior.
- Not recorded in PR evidence: whether any downstream workflow relies on the default-on metadata.

## Decision

`issue-intent` is opt-in. Each of the six intent-capable handlers enables intent only when `issue-intent: true` is set (`config.issue_intent === true`). When the flag is omitted or `false`:

- the handler runs the legacy REST path and discards intent metadata at runtime;
- `generate_safe_outputs_tools.cjs` removes `rationale`, `confidence`, and `suggest` from the tool schema and omits the intent guidance.

`issue-intent` is registered in `pkg/workflow/data/experimental_features.json`. The compiler emits "Using experimental feature: issue-intent" when any of the six outputs sets `issue-intent: true`. The frontmatter reference, the safe-outputs specification, and the runtime guide describe the feature as experimental and disabled by default. ADR-46207 is marked Superseded.

## Alternatives Considered

The PR records no alternatives evaluation. The options below come from the diff and from the alternatives listed in ADR-46207.

1. **Keep default-on (the ADR-46207 status quo).** Rejected by this PR. Experimental behavior applies to every workflow that uses these outputs, and the compiler cannot warn about it.
2. **Opt-in by documentation only.** Keep the flag disabled by default without registering the feature as experimental. Users who opt in would receive no compiler signal. ADR-46207 lists a similar option, "Keep Explicit Opt-In, Improve Documentation."
3. **Global default configuration key.** Set one default for all intent-capable outputs in the safe-outputs configuration instead of a per-handler flag. ADR-46207 lists this option. The PR does not show whether it was evaluated against the per-handler flag.

## Consequences

**Positive**

- Experimental intent behavior runs only when a workflow sets `issue-intent: true`.
- The compiler warns when the experimental feature is enabled, so its use is visible at build time.
- Workflows that do not opt in use the established REST paths for labels, assignees, issue types, issue fields, and closes.
- Tool schemas no longer advertise intent fields to agents that have not opted in.

**Negative**

- This is a behavior change. Workflows that relied on default intent metadata now get legacy behavior, and their metadata is silently discarded at runtime.
- Without the flag, agents receive no rationale or confidence guidance, so default workflows produce less justification data.
- Both the opt-in path and the legacy path must be maintained and tested. Several JavaScript tests now set `issue_intent: true` explicitly.
- The documentation and ADR-46207 must stay in sync with the default.
