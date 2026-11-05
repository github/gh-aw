# ADR-74945: Surgical YAML Scalar Edits for Import Rewrites During Workflow Updates

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: pelikhan and gh-aw maintainers (review pending)

---

### Context

`gh aw update` rewrites relative `imports:` entries in installed workflows into fully qualified, pinned workflow specs (for example `shared/control.md` → `owner/repo/workflows/shared/control.md@sha`). Before this change, `processImportsWithWorkflowSpec` (`pkg/cli/imports.go`) always rebuilt the whole frontmatter from the parsed map. It did this even when no import changed. Re-serializing dropped YAML comments, reordered keys, changed quoting, and reflowed lists (issue #73073). Users lost authored formatting, and the three-way textual merge in `pkg/cli/update_merge.go` saw spurious diffs. Installations that earlier updates had already canonicalized must still merge without conflicts.

### Decision

We will treat frontmatter as text and edit only the affected import scalars in place, instead of re-serializing the document. If no import path changes, the original content is returned verbatim. Otherwise, the frontmatter is parsed into a `yaml.v3` node tree to find each import scalar's line and column (plain list entries, `uses:`/`path:` mappings, `imports.aw`, and flow style). Each value is replaced with a correctly re-quoted value (plain, single-quoted, or double-quoted), and the result is re-parsed and checked with `reflect.DeepEqual` against the expected frontmatter. Shapes that cannot be edited safely in place (block scalars, anchors/aliases, merge keys, or a failed equivalence check) fall back to the previous serialization path. For merges, we will normalize only a legacy-canonicalized *base* (and comment-free canonical inputs) so that installations formatted by older updates keep merging without conflicts. Upstream comments and key order remain visible to the textual merge.

### Alternatives Considered

#### Alternative 1: Keep full re-serialization, improve the serializer

Make `reconstructWorkflowFileFromMap` preserve comments and order, for example by round-tripping through `yaml.Node` and re-emitting it. This was considered because it keeps one code path. It was rejected because the `yaml.v3` emitter still normalizes indentation, quoting, and flow/block style, and attaches comments imperfectly. Byte-for-byte preservation of unrelated content cannot be guaranteed.

#### Alternative 2: Regex/string replacement of import paths in raw text

Search for the old path string in the frontmatter text and replace it. This is simpler, but it is unsafe: the same string can appear in comments, `with:`/`inputs:` values, or anchors (the tests cover these cases). It also cannot handle quoting rules correctly. Edits guided by the node tree's positions avoid these false matches.

#### Alternative 3: Edit the `yaml.Node` tree and marshal the node document

This preserves more structure than marshalling a map, but it still reformats the whole frontmatter (indentation, comment placement, line wrapping). It does not meet the requirement to keep unrelated content unchanged.

### Consequences

#### Positive
- Unchanged imports produce byte-identical output, and changed imports alter only the scalar text. Comments, key order, quoting, CRLF line endings, and body content are preserved.
- Fewer spurious diffs and merge conflicts during `gh aw update`. Legacy canonicalized installations still merge without conflicts, including independent local frontmatter edits.
- A semantic equivalence check (re-parse + `DeepEqual`) guards each in-place edit, and unsupported shapes fall back to the known serialization path.

#### Negative
- Two code paths (in-place edit and serialization fallback) must be maintained and tested. Exotic YAML (block scalars, anchors on import values, merge keys) still loses formatting.
- The logic for line/column arithmetic (rune-based columns, frontmatter offset, backwards iteration for flow-style lines) and quoting is subtle, and adds complexity to `pkg/cli/imports.go`.
- Legacy merge normalization adds heuristic comparisons of canonical forms in `update_merge.go`, which future maintainers need to understand.

#### Neutral
- Adds direct dependencies on `go.yaml.in/yaml/v3` node APIs in `pkg/cli/imports.go`.
- Regression tests cover unchanged, plain, quoted, `uses`/`path`, `aw`, flow, Unicode-column, fallback, and legacy-merge cases.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
