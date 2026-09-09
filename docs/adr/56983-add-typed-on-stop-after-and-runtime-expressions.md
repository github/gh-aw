# ADR-56983: Add Typed `on.stop-after` and Runtime Expressions

**Date**: 2026-08-29
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request changes workflow frontmatter handling in `pkg/workflow` so `on.stop-after` is no longer interpreted only through the dynamic `On` map and can also accept GitHub Actions expressions such as `${{ inputs.stop-after }}`. The PR description identifies a drift risk between typed config, parser behavior, and documentation because `stop-after` was documented and consumed at runtime but had no dedicated typed field. The existing compile-time stop-after resolution logic also rejected expression-based values even when those values should be deferred to workflow runtime. Because this PR adds more than 100 lines in business-logic directories and changes parser/compiler behavior, the underlying design decision should be recorded explicitly.

### Decision

We will add a typed `OnStopAfter` field to `FrontmatterConfig`, centralize `on.stop-after` extraction in a shared parser helper, and treat GitHub Actions expressions for `stop-after` as runtime-resolved values that pass through compilation unchanged. We chose this approach to eliminate schema/parser/docs drift, keep typed and untyped frontmatter access paths consistent, and allow parameterized stop times without forcing compile-time parsing of runtime expressions. Literal relative and absolute stop-after values will continue to be resolved using the existing compiler behavior.

### Alternatives Considered

#### Alternative 1: Keep `stop-after` Dynamic-Only in `on` Map

Continue reading `on.stop-after` only from `map[string]any` and leave typed config without a dedicated field.

This was considered because it would require the fewest structural changes to frontmatter parsing. It was not chosen because the PR evidence shows this has already created typed-schema and documentation drift risk, and separate access paths make it easier for parser behavior to diverge over time.

#### Alternative 2: Require All `stop-after` Values to Be Compile-Time Literals

Preserve the existing behavior that parses every `stop-after` value as a relative delta or absolute timestamp during compilation.

This was considered because compile-time normalization gives early validation and a single resolved representation in generated workflows. It was not chosen because GitHub Actions expressions are legitimate runtime inputs for workflow dispatch and should not be rejected merely because they cannot be resolved at compile time.

### Consequences

#### Positive
- Typed frontmatter now exposes `on.stop-after` explicitly, reducing drift between config structs, parser behavior, schema text, and documentation.
- A shared parsing helper makes typed population and runtime extraction use the same interpretation logic, lowering the risk of inconsistent behavior.
- Workflows can accept expression-based `stop-after` values such as `${{ inputs.stop-after }}`, enabling runtime parameterization for dispatch inputs.

#### Negative
- Stop-after handling now has two execution modes: compile-time resolution for literals and runtime passthrough for expressions, which increases conceptual complexity.
- Expression-based values defer some validation until workflow runtime, so certain user errors will no longer be caught during compilation.
- Adding another typed frontmatter field increases the maintenance surface of `FrontmatterConfig` and its parsing/tests.

#### Neutral
- Existing literal `stop-after` formats remain supported; this change extends accepted inputs rather than replacing them.
- The implementation requires coordinated updates across parser code, schema descriptions, generated docs, and tests.
- Runtime workflow semantics change only for expression inputs; absolute and relative literal values continue through the established resolution path.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
