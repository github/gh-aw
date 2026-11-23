# ADR-80402: Make Issue-Intent an Experimental Opt-In Feature

**Date**: 2026-11-23
**Status**: Accepted
**Deciders**: pelikhan
**Supersedes**: [ADR-46207](46207-default-on-issue-intent-metadata.md)

## Context

Issue-intent metadata (`rationale`, `confidence`, `suggest`) lets an agent attach a justification and confidence level to label, assignment, issue-type, issue-field, and close actions. Under [ADR-46207](46207-default-on-issue-intent-metadata.md), this metadata was on by default for six safe-output handlers: `close-issue`, `add-labels`, `assign-to-agent`, `assign-to-user`, `set-issue-type`, and `set-issue-field`. Omitting `issue-intent` routed these calls through the GraphQL intent path and added intent guidance to the generated tool schemas.

The underlying GitHub API support is still experimental (feature-flagged GraphQL fields), but default-on behavior applied it to every workflow using these outputs, including workflows that never requested it. The compiler did not flag it as experimental, so authors had no signal that they depended on it.

Forces:

- Experimental behavior should not apply silently to workflows that did not request it.
- Authors who do opt in should be told, at compile time, that they rely on an experimental feature.
- Intent metadata is forwarded only through the GraphQL path; the REST paths do not carry it.
- Workflows that implicitly relied on the default lose metadata unless they opt in.

## Decision

`issue-intent` is an experimental, opt-in option. Each of the six intent-capable handlers uses intent only when `issue-intent: true` is set (`config.issue_intent === true`). When the option is omitted or `false`:

- `generate_safe_outputs_tools.cjs` strips `rationale`, `confidence`, and `suggest` from the tool schemas and omits the intent guidance, so agents are not prompted to supply them;
- the handlers use the non-intent path and drop any intent metadata that still arrives, logging an informational message that the metadata is ignored and that `issue-intent: true` opts in.

`issue-intent` is registered in `pkg/workflow/data/experimental_features.json`, and the compiler emits "Using experimental feature: issue-intent" when any of the six outputs sets `issue-intent: true`. The frontmatter reference, safe-outputs specification, and runtime guide describe the option as experimental and disabled by default. Repository workflows that relied on the old default (`ai-moderator`, `auto-triage-issues`, `issue-triage-agent`) now set `issue-intent: true` explicitly. A major changeset documents the migration.

## Alternatives Considered

1. **Keep default-on (ADR-46207).** Rejected. Experimental behavior would continue to apply to every workflow using these outputs, with no compile-time signal.
2. **Opt-in without an experimental warning.** Disable by default but do not register the feature as experimental. Rejected: the maintainer explicitly asked for a compiler warning so that opted-in workflows are visibly marked as depending on an experimental feature.
3. **Remove the feature entirely.** Rejected. Intent metadata is useful for triage workflows that want reviewable rationale and confidence, and the opt-in path keeps it available while the API matures.

## Consequences

**Positive**

- Experimental intent behavior runs only when a workflow sets `issue-intent: true`.
- The compiler warns when the feature is enabled, making its use visible at build time.
- Tool schemas no longer advertise intent fields to agents in workflows that have not opted in.

**Negative**

- Breaking behavior change: existing workflows that relied on the implicit default lose intent metadata unless they set `issue-intent: true` and recompile. Three repository workflows were updated for this reason.
- Workflows that opt in see an experimental-feature warning on every compile.
- Both the intent path and the non-intent path must be maintained and tested.
