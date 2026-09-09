# ADR-56599: Add Intent Field to Workflow Front Matter

**Date**: 2026-08-28
**Status**: Draft
**Deciders**: pelikhan, copilot-swe-agent

---

### Context

Workflow front matter currently supports `description`, which explains what a workflow does, but it has no first-class field for recording why the workflow exists. In this PR, the compiler, schema, serialization, generated lock-file header, docs, and editor autocomplete are all being extended around a new `intent` concept. The stated goal in the PR body is to preserve a durable, implementation-independent outcome even as workflow implementations evolve. Because this introduces a new top-level workflow contract that affects authoring, compilation, and generated outputs, the decision should be captured explicitly.

### Decision

We will add an optional top-level `intent` string field to workflow front matter and carry it through schema validation, frontmatter extraction, serialization, workflow data construction, generated lock-file comments, documentation, and editor autocomplete. We chose a dedicated field instead of overloading `description` so workflow authors can separately express the implementation-independent purpose of a workflow. In compiled output, `intent` will be rendered as a distinct `# Intent:` comment block to preserve that distinction for readers of generated `.lock.yml` files.

### Alternatives Considered

#### Alternative 1: Continue Using `description` for Both What and Why

Keep the existing schema unchanged and ask authors to encode both operational behavior and strategic purpose inside `description`.

This was considered because it avoids new schema and compiler surface area. It was not chosen because the PR evidence explicitly identifies a gap: `description` captures what a workflow does, not the durable outcome it is meant to achieve. Combining both concerns into one field makes the rationale less stable when implementation details change.

#### Alternative 2: Store Intent Only in Documentation or Markdown Body

Document a workflow's purpose in prose sections or reference docs without adding a structured frontmatter key.

This was considered because it keeps frontmatter smaller and avoids changing generated outputs. It was not chosen because unstructured documentation is harder to validate, round-trip, expose in tooling, and preserve in compiled lock-file headers. The PR adds schema validation, autocomplete, serialization, and compilation support precisely because the intent needs to be machine-visible as well as human-readable.

### Consequences

#### Positive
- Workflow authors gain a first-class, validated place to record the durable purpose of a workflow separately from its behavior.
- Tooling stays consistent because schema validation, autocomplete, serialization, compilation, docs, and tests all recognize the same field.
- Generated `.lock.yml` headers can communicate both description and intent, helping readers understand both what the workflow does and why it exists.

#### Negative
- The top-level workflow contract becomes larger, which increases maintenance burden across schema, compiler, docs, generated assets, and tests.
- Introducing a second narrative field may create author confusion or inconsistent usage unless documentation clearly explains the distinction from `description`.

#### Neutral
- Existing workflows remain valid because `intent` is optional and omitted when empty.
- The compiled output format changes by adding a new comment block, but this is metadata-only and does not alter workflow execution semantics.
- Editor and documentation artifacts must stay regenerated in sync with the schema when this field evolves.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
