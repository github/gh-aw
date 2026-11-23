# ADR-80471: Correlate Comment Trigger Warnings with Actor Admission and Integrity Filtering

**Date**: 2026-11-23
**Status**: Proposed
**Deciders**: PR author and repository maintainers (pending review)

## Context

Workflows triggered by `issue_comment`, `pull_request_review`, `pull_request_review_comment`, and the AW `slash_command` and `command` shorthands run on text written by actors that the event and `on.roles` admit. When `on.roles` admits `all`, `read`, or `triage`, that text can come from actors outside trusted repository roles. If the workflow's GitHub tools also use weak integrity filtering (`tools.github.min-integrity: none` or `unapproved`), those tools can pass such content to the agent.

Before this PR, the compiler and `gh aw lint` did not warn on this combination. The existing CTR-008 check covered only `pull_request_target`.

Forces:

- Comment-driven workflows are often intentional. A warning on the trigger alone would flag workflows that limit roles to trusted members or require approved or merged content.
- Integrity filtering protects GitHub tool inputs. It does not control event admission, arbitrary scripts, or execution of PR code, so no single control proves safety.
- Imported configuration can supply the threshold and role policy. Standalone lint does not resolve imports.
- A missing threshold can be filled by defaults or imports, including visibility-dependent defaults. Unknown thresholds, unresolved imports, and unknown repository visibility are therefore not evidence of weak filtering.

The decision prioritizes actionable warnings over trigger-only noise without claiming comprehensive protection. Actor admission does not prove arbitrary PR code or inputs from other tools are safe.

## Decision

Add the default lint rule `dangerous-triggers` as an advisory check, not an authorization mechanism. It covers CTR-039 comment-input risk and the separate CTR-008 target-trigger risk, has Warning severity, and targets both Actions and AW ASTs. The AW comment, review, and slash-command input warning requires both conditions:

1. **Actor admission is broadened.** The effective `on.roles` contains `all`, `read`, or `triage`. The protected defaults (`admin`, `maintainer`, `write`) and explicit trusted-only roles, including `maintain`, do not warn.
2. **Integrity filtering is weak.** GitHub tools are enabled and `tools.github.min-integrity` is `none` or `unapproved`, or `tools.github.lockdown: true` overrides configured guard-policy fields. Explicit thresholds are validated before considering lockdown. Without that override, explicit `approved` or `merged` filtering suppresses the comment-input warning. Disabled GitHub tools suppress it; missing or unknown thresholds, string GitHub shorthand, and bare lockdown without configured guard fields do not establish weak filtering. Lockdown must be disabled before approved/merged filtering takes effect as remediation. `integrity-proxy: false` does not bypass gateway filtering.

Related decisions:

- Compiler lint evaluates effective roles and resolved tools, including imports. Dry-run retains only authored `admin`, `maintainer`, or `maintain` roles, defaulting to `admin`, `maintainer` when none remain; compiler lint uses those restricted roles. Standalone `gh aw lint` uses authored values and does not resolve imports.
- Authored centralized commands remain analyzed alongside generated dispatch graphs without duplicate inline-command findings. AW-backed findings use AW origin paths so authored ignores apply in compilation and companion locks; graph-only findings remain Actions-owned.
- `pull_request_target` keeps its separate CTR-008 target-trigger warning, independent of actor admission and GitHub tool filtering. Only literal AW `on.pull_request_target.acknowledge-risk: true` suppresses that lint warning; it does not suppress comment-input warnings or existing validator checks. Compiler lint avoids duplicating an equivalent CTR-008 validator warning only when that warning was actually emitted, not merely because strict mode is enabled.
- The new rule emits warnings only, including in strict mode. Existing validator behavior is preserved. Generated workflow behavior, permissions, and runtime authorization are unchanged. Specification version 1.0.108 and its single unreleased changelog entry document CTR-039, T-CTR-054, and the review refinements.

## Alternatives Considered

- **Warn on every comment or review trigger.** Rejected. This flags intentional designs that restrict roles to trusted members or require approved filtering, and it adds noise without distinguishing risk.
- **Make the finding a compile error.** Rejected for this change. Blocking the known combination could enforce a stricter organizational policy, but it would break intentional comment-driven designs based on a limited static correlation. Advisory severity preserves compilation behavior while surfacing the review decision.
- **Rely only on an external GitHub Actions event allowlist.** Rejected as the sole control. An organization-level allowlist can exclude unnecessary triggers, but gh-aw cannot query or configure that policy. It complements rather than replaces local diagnostics.

## Consequences

### Positive

- Workflows that combine broadly admitted comment or review events with weak GitHub tool filtering receive a review warning at compile time and in lint.
- Trusted-role workflows and GitHub-disabled workflows avoid the comment-input warning; approved or merged filtering also avoids it unless lockdown overrides policy. These exceptions do not suppress the independent target-trigger warning.
- Compile-time lint uses resolved imports and effective roles, so imported policy is evaluated rather than missed.

### Negative

- The rule adds review noise for workflows that accept untrusted comments on purpose with weak filtering. Warnings do not block merges and do not prove safety.
- Unknown thresholds, unresolved imports in standalone lint, and unknown visibility are not classified as weak policy, so some risky workflows will not be flagged.
- Corpus evidence is partial, so the prevalence of this pattern across public workflows is unknown.

### Neutral

- Authored-policy standalone lint and resolved-policy compiler lint remain distinct paths that require regression coverage.
- Warning deduplication depends on actual validator emission, preserving a target-trigger diagnostic when the validator did not emit an equivalent warning.

## Evidence and Validation Limits

The previously verified public corpus contained 26 authored sources and 26 generated locks across six repositories. Static checks did not execute workflows or resolve imports. Findings fell from eight to zero after actor-role and unknown-policy refinement; this supports reduced false-positive noise in that sample, not universal safety. Discovery was capped and had 30 metadata-access gaps, so the sample is not an exhaustive ranking or prevalence estimate.

Previously reported local build, lint, schema, and impacted-test checks passed. At that earlier revision, the repository gate remained blocked by unrelated workflow-lock drift. That historical result is not the validation status of the current revision; neither it nor the corpus check establishes live/runtime validation.

## References

- [PR #80471](https://github.com/github/gh-aw/pull/80471)
- [Workflow linter reference](../src/content/docs/reference/workflow-linters.md)
- [dangerous-triggers implementation](../../pkg/workflow/lint/dangerous_triggers.go)
- [Alpha-Omega event-policy guidance](https://alpha-omega.dev/blog/de-fanging-dangerous-triggers-in-github-actions-across-an-organization/) (external context, not implementation evidence)
