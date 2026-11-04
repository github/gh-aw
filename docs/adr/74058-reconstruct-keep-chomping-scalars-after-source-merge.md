# ADR-74058: Rebuild Keep-Chomping Scalar Newlines After Source Merging

**Date**: 2026-11-02
**Status**: Proposed (pending maintainer review)
**Deciders**: Pending maintainer review; no approval recorded

---

### Context

`pkg/workflow/actions` edits workflow YAML while retaining comments, quoting, and layout. Its `goccy/go-yaml` AST renderer trims terminal line feeds from block-scalar output. Keep-chomping scalars (`|+` and `>+`) retain those line feeds as part of their parsed value, so source edits must reconstruct the missing suffix rather than silently change that value.

Previously, `sourceLiteralIndentVisitor` reconstructed trailing newlines during indentation shifts. That phase does not have the final merged syntax needed to determine whether a following sibling contributes a blank line. Repository workflow round-trip checks exposed scalar-value changes, including failures in two generated workflows during an attempted zero-indent-shift workaround.

### Decision

Run `preserveSourceLiteralNewlines` in `emitWithValidation` after `mergeSourceNode` and before rendering. Keep indentation shifting separate from trailing-newline reconstruction.

The post-merge pass visits scalars through mappings, sequences, anchors, and tags. It reconstructs keep-chomping token origins using the scalar value and final surrounding syntax, accounting for blank lines already rendered by a following sibling and for intervening foot comments. Reconstruction must preserve the complete scalar value, including significant whitespace on suffix lines such as excess indentation on whitespace-only lines, not merely its terminal line-feed count.

Preserve the original suffix data rather than rebuilding every whitespace-only line with one fixed indent. Use lossless JSON snapshots for semantic validation and native workflow snapshots: the YAML block-style encoder can independently discard suffix whitespace before source merging or validation. For `EditSource` merge inputs, retain YAML block mappings and plain keys while JSON-quoting multiline scalar values. Whole-document JSON snapshots would otherwise introduce quoted keys and flow mappings into newly inserted YAML syntax.

Retain `validateSourceEmission`: parse the emitted workflow and compare its serialized Actions AST with the requested AST. Return an error on mismatch rather than accept a semantically changed edit. This validation is a source-editing correctness check, not authorization or a runtime safety verdict.

### Alternatives Considered

#### Retain reconstruction in the indentation-shift visitor

This was the previous implementation. It couples newline repair to indentation changes before the final neighbouring syntax is settled, even though a sibling's rendered blank lines affect the required suffix.

#### Skip indentation handling when the column shift is zero

This smaller workaround was tried, but failed round-trip checks for two actual generated workflows and some edited cases. A zero column shift does not establish that the renderer will preserve the scalar value.

#### Reconstruct after merging with rendered-neighbour accounting (selected)

Moving reconstruction after the source merge makes the final sibling context available. Accounting for already-rendered blank lines avoids duplicating those newlines, while semantic validation rejects remaining mismatches. This keeps the correction within the existing source editor and preserves author-owned scalar styles.

### Consequences

The source editor has one post-merge reconstruction phase instead of repairing scalar suffixes during indentation shifts. Keep-chomping scalar styles can remain intact, and semantic validation continues to reject emitted values that differ from the requested workflow.

The emitter gains another AST traversal and remains coupled to `goccy/go-yaml` rendering and token-position behaviour. Rendered-neighbour detection is not a general YAML formatting model; changes to upstream rendering or unsupported layouts can still cause source editing to fail validation.

Scalar regression tests and repository workflow round-trip checks must cover edits as well as unchanged input, including whitespace-only suffixes, nesting, sibling blank lines, comments, and line endings. These tests are evidence for their covered cases, not a claim that every YAML layout is supported. The source-editing API is unchanged.
