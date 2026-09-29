# ADR-64179: Narrow add-labels item schemas

**Date**: 2026-09-29
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds support for workflow authors to constrain the item schema exposed by the `add_labels` safe output tool. Before this change, a workflow could document stricter per-label requirements, but the generated MCP tool schema and call-time validation still used the broader built-in schema, so invalid structured label payloads were only rejected after agent execution. The implementation spans workflow config parsing, schema generation, compiler validation, runtime tool generation, tests, and documentation, which indicates a cross-cutting design decision rather than a local refactor. The decision is how gh-aw should let workflows express stricter `add_labels` payload requirements without allowing incompatible schema widening.

### Decision

We will add a configurable `safe-outputs.add-labels.item-schema` that can only narrow the built-in `add_labels` label item schema, then use that normalized schema in both generated tool metadata and compiler-time validation. We will reject schema changes that widen built-in types, fields, or enum values, and we will replace the `labels` array item schema in the generated tool definition with the normalized narrowed schema. We chose this because it lets workflows surface stricter agent-facing contracts early enough for MCP validation and retries while preserving compatibility with the underlying safe output semantics.

### Alternatives Considered

#### Alternative 1: Keep only the built-in broad label schema

This was a realistic option because it preserves the current string-or-object label contract and requires no extra validation machinery. It was not chosen because the PR description and code show that broader schemas let agents produce payloads that violate workflow-specific requirements, with failures surfacing too late for effective agent retry behavior.

#### Alternative 2: Allow arbitrary replacement schemas for `add_labels` items

This was considered because it would give workflow authors maximum flexibility over the label object shape. It was not chosen because the implementation explicitly guards against widening built-in fields and values, and arbitrary replacement would let workflows define incompatible payloads that the safe output runtime does not actually support.

### Consequences

#### Positive
- Agents see the stricter `add_labels` item contract directly in the generated tool schema, so invalid calls can fail during MCP validation instead of after execution.
- Workflow authors can require existing fields, drop optional fields, and narrow enums while staying compatible with the built-in safe output behavior.
- The compiler and runtime now share a normalized schema path, reducing drift between documented configuration and enforced validation.

#### Negative
- The add-labels configuration path becomes more complex because it now includes schema normalization, compatibility checks, and metadata propagation.
- Future changes to the built-in label schema must remain synchronized with the narrowing validator and related tests.
- Workflow authors cannot define entirely new label item fields, which may feel restrictive for some custom workflows.

#### Neutral
- The change only affects `add_labels`; related tools such as `remove_labels` continue to accept structured label objects but ignore intent metadata fields.
- Tool metadata generation now supports array item schema overrides, which may be reused by future safe output tools but also broadens that internal extension surface.
- Documentation and JSON schema references now need to describe narrowing rules alongside existing safe output configuration options.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
