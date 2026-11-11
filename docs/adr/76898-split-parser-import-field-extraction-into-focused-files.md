# ADR-76898: Split Parser Import Field Extraction into Focused Files

**Date**: 2026-11-11
**Status**: Draft
**Deciders**: [TODO: verify] PR #76898 was authored by `copilot-swe-agent` from issue #76861. Reviewers to confirm who decided this.

---

### Context

`pkg/parser/import_field_extractor.go` had grown to 1,190 lines. It mixed import orchestration, configuration extraction, activation merging, model normalization, and result building in one file. Frontmatter fields from imported workflows are accumulated into a single `importAccumulator` during BFS import traversal, so all of this logic shares one state type. The file also converted JSON strings to bytes just to unmarshal them while rejecting trailing JSON. Developers working in this area need to find logic by responsibility.

### Decision

We will split the file by responsibility within package `parser`, keeping the `importAccumulator` method set and import behavior unchanged. Accumulator and result construction move to `import_accumulator.go`. Frontmatter handling moves to `import_extract_frontmatter.go`. Engine and config extraction move to `import_extract_engine.go`. Activation fields move to `import_extract_activation.go`. Model normalization moves to `import_extract_models.go`. Step and feature extraction move to `import_extract_steps.go`. We will also add a shared `unmarshalJSONFromString` helper in `import_extract_helpers.go` that uses `json.Decoder` and rejects trailing JSON values. The primary driver is maintainability: each new file stays under 300 lines.

### Alternatives Considered

#### Alternative 1: Keep one file and trim it in place

Shorten functions inside `import_field_extractor.go` and leave its mixed responsibilities alone. This is the smallest diff. It was not chosen because it would not reduce the file's size or its mix of responsibilities. This rationale is inferred from the PR body. [TODO: verify]

#### Alternative 2: Move the code into a new sub-package

A sub-package such as `pkg/parser/imports/` would enforce boundaries at compile time. It was not chosen because the accumulator's fields and helpers are package-private and shared with the rest of `parser`. A sub-package would require exporting them and changing call sites. This rationale is inferred from the diff. [TODO: verify]

### Consequences

#### Positive

- Each file has one responsibility and is under 300 lines (largest: `import_extract_engine.go`, 272 lines), so navigation cost drops.
- No package boundary changes, so callers in package `parser` need no edits.
- The shared `unmarshalJSONFromString` helper gives import extraction one strict-decoding path, with tests for trailing whitespace, a second JSON value, and malformed trailing content.
- The helper avoids the string-to-byte conversion that the previous unmarshal path used.

#### Negative

- Moved code loses per-line blame history in the new files unless reviewers use `git blame -C`.
- The `import_*` file family in `pkg/parser` grows, so the new names are easy to confuse with related files.
- The diff is large (about 1,200 added lines) and mixes a mechanical move with a small behavior change in JSON decoding. This makes review harder.
- Relocated lines count toward the gate's business-logic volume threshold, so a refactor triggers ADR enforcement by volume alone.

#### Neutral

- All new files stay in `package parser`, and each has its own import block.
- The move is intended to be behavior-preserving. The strict-decoding helper now rejects trailing JSON consistently, and this ADR does not record a test run for this PR.
- The decision is inferred from the PR body and diff. Rationale for the alternatives is marked `[TODO: verify]`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
