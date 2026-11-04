# ADR-74948: Preserve the Requested Ref in the Workflow `source:` Field

**Date**: 2026-11-05
**Status**: Draft
**Deciders**: [TODO: verify] PR author and gh-aw maintainers

---

### Context

`gh aw add owner/repo/workflows/x.md@v1.2.0` wrote `source: owner/repo/workflows/x.md@<40-char sha>` because `buildSourceStringWithCommitSHA` (`pkg/cli/spec.go`) chose the resolved commit SHA over the ref the user asked for. `gh aw update` reads the ref in `source:` to decide how to update. When that ref is a commit SHA, it fetches the latest commit from the default branch. As a result, a workflow installed from a release tag quietly started following unreleased `main`. That contradicts the documented behavior: exact tags stay pinned and `@v1` follows releases (issue #73077). Manifest installs (`FromRepositoryManifest`) had the same problem. Separately, dependency fetching could fill in a default ref on the shared `WorkflowSpec` and change the recorded source intent.

### Decision

We will treat `source:` as a record of what the user *intends to track*, not of the exact commit that was installed. `buildSourceStringWithCommitSHA` will write the ref the user gave (a tag, major ref, branch, or SHA) exactly as given. It will use the resolved commit SHA only when no ref was given. The same rule applies to both per-file workflow sources and repository-manifest sources. To protect that intent, `fetchWorkflowDependencies` passes a copy of the `WorkflowSpec` to `fetchAllRemoteDependencies`, so dependency resolution cannot change the requested ref. The main reason is that `update` must follow the user's chosen release channel. Exact installed-commit pinning is already handled elsewhere: the lock file and `.github/aw/imports/` pin by SHA.

### Alternatives Considered

#### Alternative 1: Keep the SHA in `source:` and teach `update` to recover the original ref

Keep writing the commit SHA and change `update` to map the SHA back to a tag, for example by looking up which release tags point at that commit. This keeps an exact pin in frontmatter. However, it adds network lookups and guesswork: several tags can point at the same commit, and the commit cannot tell a branch from a tag. It also cannot tell whether the user wanted `v1` or `v1.2.0`. The user's intent is lost at write time, so later inference can't recover it reliably.

#### Alternative 2: Preserve only semver/major tags, and keep using the SHA for branches

The issue's implementation plan suggested keeping `workflow.Version` only when it is a release tag (`isSemanticVersionTag`) or a major ref like `v1`, and using the SHA otherwise. That is a narrower change. However, it would still turn branch installs (for example `@release/stable`) into SHA pins. Those pins would then follow the default branch on update instead of the requested branch, which is the same class of bug. The PR keeps every explicit ref.

#### Alternative 3: Record both the ref and the SHA in frontmatter

Write the requested ref to `source:` and add another field (or a lock-file metadata entry) for the resolved SHA, for auditing. The issue lists this as optional. It was not done in this PR. The lock file and import cache already pin by SHA, so a second frontmatter field would add more format surface for little gain. [TODO: verify whether audit metadata is still wanted]

### Consequences

#### Positive
- Workflows installed at a release tag or major ref update along compatible releases (for example `v1.2.0` → `v1.4.0`, not `v2.0.0`) and no longer follow the default branch. A regression test in `update_workflows_test.go` covers this.
- `source:` now matches the documented examples in `reference/frontmatter.md` and `guides/sharing-workflows.md`, so users no longer need to edit it by hand after `add`.
- Per-file and manifest sources follow the same ref-selection rule. This removes a separate code path that behaved differently.

#### Negative
- `source:` no longer records the exact commit installed when a mutable ref (a tag that can be moved, or a branch) was given. Users who want reproducibility must rely on the lock file and import cache SHAs, or install with an explicit SHA.
- This changes behavior for existing users. Newly added workflows that used a branch ref (for example `@main`) now record `@main` instead of a SHA, so `update` will follow that branch. Users who expected SHA pinning from a branch install will see different update targets.
- Copying the `WorkflowSpec` for dependency fetching relies on a convention, not a type guarantee. Future code that changes the shared spec could bring the bug back.

#### Neutral
- Installs with no ref keep the earlier behavior: the resolved commit SHA is recorded.
- Workflows already installed with SHA-based `source:` values are not migrated. They keep their current update behavior until the user edits `source:` or re-adds the workflow.
- The function keeps its name `buildSourceStringWithCommitSHA`, even though it now writes the SHA only as a fallback.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
