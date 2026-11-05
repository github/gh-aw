# ADR-74948: Preserve the Requested Ref in the Workflow `source:` Field

**Date**: 2026-11-05
**Status**: Draft
**Deciders**: [TODO: verify] PR author and gh-aw maintainers

---

### Context

`gh aw add owner/repo/workflows/x.md@v1.2.0` wrote `source: owner/repo/workflows/x.md@<40-char sha>` because `buildSourceStringWithCommitSHA` (`pkg/cli/spec.go`) chose the resolved commit SHA over the ref the user asked for. `gh aw update` reads the ref in `source:` to decide how to update. When that ref is a commit SHA, it fetches the latest commit from the default branch. As a result, a workflow installed from a release tag quietly started following unreleased `main`. That contradicts the documented behavior: exact tags stay pinned and `@v1` follows releases (issue #73077). Manifest installs (`FromRepositoryManifest`) had the same problem. Separately, dependency fetching could fill in a default ref on the shared `WorkflowSpec` and change the recorded source intent.

### Decision

`source:` records what the user intends to track, not the exact commit installed. `buildSourceStringWithCommitSHA` writes the explicit tag, major ref, branch, or SHA, falling back to the resolved commit only when no ref was given. This applies to both per-file workflow sources and repository-manifest sources.

The optional `source-sha:` field retains the installed commit as the immutable three-way merge base. Package ownership records retain the tracking source and resolved commit separately. The updater excludes managed source metadata from content comparisons and advances the installed revision after merging. Existing files without revision metadata retain the previous ref-based fallback.

Dependency fetching uses a copy of `WorkflowSpec` pinned to the fetched workflow commit, preserving both source intent and installation consistency. Release resolution propagates cooldown blocking so manifest updates cannot synchronize resources from a moved fallback tag when no eligible upgrade exists.

### Alternatives Considered

#### Alternative 1: Keep the SHA in `source:` and teach `update` to recover the original ref

Keep writing the commit SHA and change `update` to map the SHA back to a tag, for example by looking up which release tags point at that commit. This keeps an exact pin in frontmatter. However, it adds network lookups and guesswork: several tags can point at the same commit, and the commit cannot tell a branch from a tag. It also cannot tell whether the user wanted `v1` or `v1.2.0`. The user's intent is lost at write time, so later inference can't recover it reliably.

#### Alternative 2: Preserve only semver/major tags, and keep using the SHA for branches

The issue's implementation plan suggested keeping `workflow.Version` only when it is a release tag (`isSemanticVersionTag`) or a major ref like `v1`, and using the SHA otherwise. That is a narrower change. However, it would still turn branch installs (for example `@release/stable`) into SHA pins. Those pins would then follow the default branch on update instead of the requested branch, which is the same class of bug. The PR keeps every explicit ref.

### Consequences

#### Positive
- Workflows installed at a release tag or major ref update along compatible releases (for example `v1.2.0` → `v1.4.0`, not `v2.0.0`) and no longer follow the default branch. A regression test in `update_workflows_test.go` covers this.
- `source:` now matches the documented examples in `reference/frontmatter.md` and `guides/sharing-workflows.md`, so users no longer need to edit it by hand after `add`.
- Per-file and manifest sources follow the same ref-selection rule. This removes a separate code path that behaved differently.

#### Negative
- Tracking intent and installed revision now occupy separate fields. The installed SHA must advance with successful updates and remain unchanged when an update is skipped or rolled back.
- This changes behavior for existing users. Newly added workflows that used a branch ref (for example `@main`) now record `@main` instead of a SHA, so `update` will follow that branch. Users who expected SHA pinning from a branch install will see different update targets.
- Copying the `WorkflowSpec` for dependency fetching relies on a convention, not a type guarantee. Future code that changes the shared spec could bring the bug back.

#### Neutral
- Installs with no ref keep the earlier behavior: the resolved commit SHA is recorded.
- Workflows already installed with SHA-based `source:` values are not migrated. They keep their current update behavior until the user edits `source:` or re-adds the workflow.
- The function keeps its name `buildSourceStringWithCommitSHA`, even though it now writes the SHA only as a fallback.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
