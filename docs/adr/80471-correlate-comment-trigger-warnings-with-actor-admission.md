# ADR-80471: Correlate Comment Trigger Warnings with Actor Admission and Integrity Filtering

**Date**: 2026-11-23
**Status**: Draft

## Context

Workflows triggered by `issue_comment`, `pull_request_review`, `pull_request_review_comment`, and the AW `slash_command` and `command` shorthands run on text written by actors that the event and `on.roles` admit. When `on.roles` admits `all`, `read`, or `triage`, that text can come from actors outside trusted repository roles. If the workflow's GitHub tools also use weak integrity filtering (`tools.github.min-integrity: none` or `unapproved`), those tools can pass such content to the agent.

Before this PR, the compiler and `gh aw lint` did not warn on this combination. The existing CTR-008 check covered only `pull_request_target`.

Forces:

- Comment-driven workflows are often intentional. A warning on the trigger alone would flag workflows that limit roles to trusted members or require approved or merged content.
- Integrity filtering protects GitHub tool inputs. It does not control event admission, arbitrary scripts, or execution of PR code, so no single control proves safety.
- Imported configuration can supply the threshold and role policy. Standalone lint does not resolve imports.
- A missing threshold can be filled by defaults or imports, and the PR notes that public repositories can apply approved filtering automatically. Unknown policy is therefore not treated as weak filtering.

Not established by the PR evidence: how many downstream repositories will newly receive warnings, and whether any strict-mode policy changes. The corpus regression covered 26 workflows from six projects, with a capped discovery sample and 30 metadata-access gaps. Its prevalence results are not exhaustive.

## Decision

Add the default lint rule `dangerous-triggers`. It maps to CTR-039, correlates with CTR-008, has Warning severity, and targets both Actions and AW ASTs. It warns on a comment or review trigger only when both conditions hold:

1. **Actor admission is broadened.** The effective `on.roles` contains `all`, `read`, or `triage`. The protected defaults (`admin`, `maintainer`, `write`) and explicit trusted-only roles, including `maintain`, do not warn.
2. **Integrity filtering is weak.** `tools.github.min-integrity` is `none` or `unapproved`, or `tools.github.lockdown: true` overrides the guard policy. Explicit `approved` or `merged` filtering, `tools.github: false`, and missing or unknown thresholds do not warn.

Related decisions:

- Compiler lint evaluates effective roles and resolved tools, including imports. Standalone `gh aw lint` uses authored values.
- `pull_request_target` keeps its CTR-008 target-trigger warning. AW `on.pull_request_target.acknowledge-risk: true` suppresses only that warning. In strict mode, the existing compiler CTR-008 warning remains, and lint does not duplicate it.
- The rule emits warnings only. Generated workflow behavior, permissions, and runtime authorization are unchanged. Specification version 1.0.109 and the changelog document CTR-039 and T-CTR-054.

## Alternatives Considered

- **Warn on every comment or review trigger.** Rejected. This flags intentional designs that restrict roles to trusted members or require approved filtering, and it adds noise without distinguishing risk.
- **Make the finding a compile error.** Rejected for this change. A hard failure would turn a heuristic correlation into a build break, even though filtering does not prove that PR code is safe. Warning severity keeps the change non-breaking while still surfacing the risk.
- **Rely only on an external GitHub Actions event allowlist.** Rejected as the sole control. The Alpha-Omega guidance recommends an explicit event allowlist, but gh-aw cannot query or configure that policy, so it can only be documented, not verified. The PR documents it as an external control.

## Consequences

Positive:

- Workflows that combine broadly admitted comment or review events with weak GitHub tool filtering receive a review warning at compile time and in lint.
- Trusted-role, approved or merged, and GitHub-disabled workflows are not flagged, which limits noise for intentional designs.
- Compile-time lint uses resolved imports and effective roles, so imported policy is evaluated rather than missed.

Negative:

- The rule adds review noise for workflows that accept untrusted comments on purpose with weak filtering. Warnings do not block merges and do not prove safety.
- Missing thresholds, unresolved imports in standalone lint, and repository visibility produce no warning, so some risky workflows will not be flagged.
- Two policy paths (authored values in standalone lint, resolved values in compilation) add maintenance surface. The PR's tests cover both.
- Corpus evidence is partial, so the prevalence of this pattern across public workflows is unknown.
