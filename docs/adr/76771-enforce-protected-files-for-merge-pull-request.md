# ADR-76771: Enforce Protected-File Checks for merge-pull-request

**Date**: 2026-11-11
**Status**: Implemented; maintainer sign-off not recorded
**Decision source**: User direction for [PR #76771](https://github.com/github/gh-aw/pull/76771)

## Context

`merge-pull-request` merges a pull request on behalf of a workflow. Previously, its protected-file safeguard relied on caution notices in the PR rather than inspecting the actual changed files. A PR without those notices could therefore merge protected files into an otherwise eligible feature branch. `create-pull-request` already defines the protected set, matching rules, and exclusions; a separate merge definition would risk drift.

The user directed this change to reuse those rules, block protected-file changes by default, provide an explicit `allowed` opt-out, and exclude creation-only review and issue fallback policies. This record documents the implemented direction, not independent human approval or completed maintainer sign-off.

## Decision

Inspect the complete live PR file list using paginated API results, including deleted paths and both sides of renames. Reuse `create-pull-request` protection matching and both handler and repository exclusions. Unless trusted configuration resolves to `protected-files: allowed`, refuse protected changes and fail closed on missing, malformed, duplicate, or incomplete evidence, including changed-file counts above GitHub's 3000-file listing limit.

Repeat file protection during final revalidation before either a merge API call or staged success. GitHub's merge API atomically fences only the evaluated head SHA; revalidation narrows but does not eliminate races involving other mutable policy state.

Accept `blocked` (the default), `allowed`, GitHub Actions expressions, and the object form with `policy` and scoped `exclude` entries. Do not support `request-review` or `fallback-to-issue` for merging. Explicit `allowed` skips file inspection but preserves existing caution vetoes, branch restrictions, reviews, and other merge gates. Human approvers remain optional on eligible feature branches unless `required-approvers` is configured.

Treat the default-on behavior change as breaking and use a major changeset, consistent with the release policy. Existing workflows can retain blocking and use manual merge, configure scoped exclusions, or explicitly opt out when intentional; configuration changes require recompilation.

## Alternatives Considered

These are concrete design tradeoffs, not claims that each option was historically proposed or approved.

- **Retain caution-only protection.** Adds no file-list API calls and preserves prior behavior, but leaves unmarked protected changes unchecked.
- **Check files only during initial evaluation.** Reduces API calls, but misses evidence changes before live or staged success. Final revalidation provides a narrower race window.
- **Make blocking opt-in.** Avoids a breaking default change, but leaves existing workflows exposed to the gap. It does not satisfy the user's default-blocking direction.
- **Adopt creation-only `request-review` and `fallback-to-issue`.** Would reuse policy names, but needs a separate merge-time review or issue lifecycle rather than the creation handler's behavior. The user explicitly excluded these policies; manual merge remains available.
- **Maintain a merge-specific protected set.** Allows independent tuning, but can drift from creation protection. Shared matching and scoped exclusions provide consistency without another definition.

## Consequences

Protected changes no longer depend on notice markers for enforcement. Shared matching and exclusions keep creation and merge protection consistent, and unverifiable evidence cannot authorize a merge. Existing caution vetoes remain independent of file protection, including when `allowed` is configured.

Previously eligible merges may now be refused by default, requiring migration or manual handling. Scoped exclusions and the explicit opt-out entrust policy authors with weakening this gate; they do not establish human approval or clear caution notices. PRs exceeding 3000 changed files cannot pass the enabled file gate. Initial and final checks add paginated API calls and can refuse a merge when evidence changes or becomes unavailable.

The handler records `changed_files` in its policy snapshot and emits `protected_files_modified` for protected changes. Other mutable policy evidence can still change after final revalidation because the merge API only fences the head SHA.

This ADR does not certify test results, reviewer approval, or live merge validation. Validation evidence belongs in the PR; no live merge or workflow dispatch is authorized by this decision.
