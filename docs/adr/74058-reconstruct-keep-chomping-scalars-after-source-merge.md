# ADR-74058: Rebuild Keep-Chomping Scalar Newlines After Source Merging

**Date**: 2026-05-14
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify other reviewers]

---

### Context

`pkg/workflow/actions` edits workflow YAML at the source level so comments, quoting, and layout stay intact. The source editor uses the `goccy/go-yaml` AST, and `LiteralNode.String` drops terminal newlines from block scalars. A keep-chomping (`|+` / `>+`) value like `"first\n\n"` therefore loses trailing blank lines unless they are re-encoded in the token origin. Previously, that re-encoding happened inside `sourceLiteralIndentVisitor`, during indentation shifts. At that point the editor cannot tell whether the next sibling key will contribute its own blank line through its token positions. As a result, edits such as renaming a workflow changed the parsed value of keep-chomping scalars, and CI round-trip checks failed.

### Decision

We will remove trailing-newline reconstruction from the indentation-shift visitor. Instead, a dedicated post-merge pass, `preserveSourceLiteralNewlines`, will run in `emitWithValidation` after syntax merging. The pass walks mappings, mapping values, sequences, anchors, and tags. For each keep-chomping literal, it works out whether the following sibling already supplies a leading blank line (and whether a foot comment intervenes). It then rebuilds the literal's origin with exactly the number of indented blank lines still needed. The main reason is correctness: the right number of newlines depends on the surrounding syntax, which is only known once merging is complete.

### Alternatives Considered

#### Alternative 1: Keep the fix inside the indentation-shift visitor

Patch the existing logic in `sourceLiteralIndentVisitor` to adjust the newline count. This would have been a smaller change, but the visitor runs before the final neighbouring nodes are settled. It cannot reliably tell whether the next key carries a blank line, so it would keep over- or under-counting newlines in some layouts (nested mappings, sequences, trailing comments).

#### Alternative 2: Re-emit keep-chomping scalars in another style

Convert affected `|+`/`>+` scalars to quoted or explicit-escape strings on edit, which avoids the renderer's newline trimming entirely. This was rejected because it rewrites author-owned formatting, which defeats the purpose of source-preserving edits. It would also produce noisy diffs across repository workflows.

#### Alternative 3: Patch or replace the upstream YAML renderer

Change or fork `goccy/go-yaml` so `LiteralNode.String` keeps terminal newlines. This would fix the problem at its root, but it adds the maintenance cost of a forked dependency (or waiting on upstream). The CI failure needed a local fix.

### Consequences

#### Positive
- Keep-chomping literal and folded scalars keep their parsed value across source edits, including nested mappings, sequences, comments, and CRLF input.
- Newline handling lives in one AST pass that runs after merging, so indentation shifting and newline preservation are now separate concerns.

#### Negative
- The source emitter gains another full AST traversal, and it depends on how `goccy/go-yaml` renders literals and token positions internally. An upstream change to that behaviour could break this logic without any compile error.
- The sibling blank-line detection (`strings.HasPrefix(next.String(), "\n")`) is a heuristic. Node types the pass does not visit could still mishandle edge cases.

#### Neutral
- Regression tests in `source_scalar_test.go` and the repository-wide round-trip tests now set the expected behaviour, so future changes to the source editor must keep them passing.
- The change is limited to `pkg/workflow/actions` and does not change the source-editing API.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
