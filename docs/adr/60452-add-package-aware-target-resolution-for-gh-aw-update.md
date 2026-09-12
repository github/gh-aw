# ADR-60452: Add Package-Aware Target Resolution for gh aw update

**Date**: 2026-09-12
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The PR changes `gh aw update` so its positional arguments no longer refer only to workflow names. The diff adds a new package-target resolution path that inspects installed package ownership records under `.github/aw/packages`, matches repository package identifiers and GitHub URLs, and reapplies every workflow and managed asset owned by the selected package. The implementation also preserves custom workflow directories and engine-specific skill and agent locations, restores missing package workflows, and reports package-like targets that are not installed. The architectural question is how the update command should interpret package-shaped arguments while preserving the existing workflow-target behavior.

### Decision

We will make `gh aw update` accept installed package identifiers and GitHub package URLs as first-class positional targets, resolve them through local package ownership records, and update all workflows and managed assets owned by that package. We decided to treat package targets separately from workflow targets, derive package-specific workflow and engine context from the ownership manifest, and route package updates through the existing manifest-managed update path. This keeps the CLI aligned with how installed packages are tracked locally while restoring package-owned resources consistently instead of requiring users to update each workflow individually.

### Alternatives Considered

#### Alternative 1: Keep `gh aw update` workflow-only and require package reapplication through another command

This preserves the previous contract where every positional argument names a workflow. It was considered because it avoids expanding argument resolution rules inside an already large update path. It was not chosen because the PR evidence shows package ownership metadata already exists locally, and forcing users to update package-managed workflows one at a time would not restore missing workflows or non-workflow managed assets owned by the package.

#### Alternative 2: Resolve package targets by scanning only installed workflow files

Another option would be to infer package membership solely from workflow source frontmatter on disk and update only those discovered workflows. It was considered because it would reuse existing workflow scanning logic with fewer new code paths. It was not chosen because the PR explicitly needs to restore missing package workflows and reconcile package-managed assets such as skills and agents, which cannot be recovered reliably from currently present workflow files alone.

#### Alternative 3: Accept package targets but require remote lookup instead of local ownership records

The command could have treated package identifiers or URLs as remote references and resolved them directly from upstream package metadata. It was considered because package targets are naturally repository-based. It was not chosen because the diff is grounded in local ownership records, which let the command verify that the package is actually installed, preserve local install destinations, and avoid reapplying files into incorrect workflow or engine locations.

### Consequences

#### Positive
- Users can reapply an installed package with a single `gh aw update` target, including restoring missing workflows and package-managed assets.
- The update path now preserves custom workflow directories and engine-specific skill and agent destinations by deriving install context from ownership records.
- Package-like arguments that are not installed now fail explicitly instead of being misinterpreted as workflow names.

#### Negative
- The update command now has more complex target classification and package-resolution logic, increasing maintenance cost in an already large CLI code path.
- Correct behavior depends on local package ownership records being present and accurate; stale metadata could cause failed or incomplete package reapplication.
- Package URL matching and mixed workflow/package target handling introduce more edge cases that need ongoing test coverage.

#### Neutral
- Existing workflow-name targets continue to use the prior workflow discovery and update flow when arguments are not package-like.
- Manifest-managed package updates now reuse the existing grouped update mechanism with package-specific options layered on top.
- The change broadens CLI semantics but does not introduce a new top-level command or alter the source-frontmatter format.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
