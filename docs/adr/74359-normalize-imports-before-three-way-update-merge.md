# ADR-74359: Normalize Imports Before Three-Way Update Merge

**Date**: 2026-11-03
**Status**: Draft
**Deciders**: [TODO: verify] PR author and gh-aw maintainers

---

### Context

`gh aw update` refreshes source-managed workflows with a three-way merge of the upstream base, the local file and the new upstream version (`MergeWorkflowContent` in `pkg/cli/update_merge.go`). Before this change, only the merge output went through import processing (`processImportsWithWorkflowSpec`), which rewrites frontmatter into canonical YAML. As a result, a local file written by an earlier update had different formatting from its upstream base, for example `- item` instead of `  - item` under `imports:`. A later update could then conflict on imports that had not semantically changed. CRLF line endings caused the same kind of false conflict. Also, when a real frontmatter conflict happened, `updateManifestManagedWorkflow` updated the managed `source` field with `UpdateFieldInFrontmatter`. That function parses YAML, so it failed on conflict markers and reported a misleading error (issue #74224).

### Decision

We will run the same import canonicalization (`normalizeImportsForMerge`, which wraps `processImportsWithWorkflowSpec`) on all three merge inputs before the three-way merge. The base and local inputs use the old source spec; the upstream input uses the new source spec. We will also strip `\r` in `stringutil.NormalizeWhitespace`, so CRLF and LF content compare as equal. When the merge result contains conflicts, we will update the `source` field with the existing raw, line-based `updateTopLevelFieldInFrontmatterRaw` instead of the YAML-parsing editor. That way, the conflicted file can still be saved and reported. The main reason for this decision is to stop format-only differences that gh-aw itself creates from showing up as user conflicts.

### Alternatives Considered

#### Alternative 1: Stop rewriting imports into canonical YAML on update

This would keep the author's original frontmatter formatting, so the representations would not drift in the first place. We did not choose it because import rewriting (pinning refs and resolving paths) is existing behaviour that other parts of update rely on. Changing it would need a format-preserving YAML editor for imports, which is a larger change. [TODO: verify whether this was evaluated]

#### Alternative 2: Semantic (YAML-aware) merge of frontmatter instead of line-based diff3

A merge that parses frontmatter and compares it structurally would ignore formatting differences entirely. We did not choose it because it would replace the existing line-based merge engine, and it still could not represent conflicts inside YAML without a custom conflict format. Normalizing the inputs fixes the specific false conflicts with a localized change.

#### Alternative 3: Strip conflict markers or fail fast before updating `source` on conflict

On conflict, the update could skip writing `source` or abort. We did not choose it because the PR's goal is to save the conflicted workflow, with the new `source` pin, so the user can resolve it. The raw field updater already exists and does not need valid YAML.

### Consequences

#### Positive
- Repeat updates no longer create false conflicts from import formatting that gh-aw itself rewrote, or from CRLF/LF differences.
- Real frontmatter conflicts are saved with conflict markers and an updated `source`, instead of failing with a misleading YAML parse error.

#### Negative
- Each merge now runs import processing three times. This adds cost, and if normalizing any input fails, the merge aborts with a new error path (`failed to normalize ... imports`).
- The merge result depends on `processImportsWithWorkflowSpec` producing the same canonical output for every input. Future changes to import rewriting could bring back drift or affect merges.
- Two `source` update paths now exist (YAML-aware and raw). Their behaviour may differ on edge cases such as comments and quoting.

#### Neutral
- `NormalizeWhitespace` now trims trailing `\r` for every caller, not only for the update merge.
- Regression tests cover rewritten-import formatting, CRLF input and preserving frontmatter conflicts during manifest updates.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
