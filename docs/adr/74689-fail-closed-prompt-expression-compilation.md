# ADR-74689: Fail-Closed Prompt Expression Compilation

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: pelikhan and gh-aw maintainers (review pending)

---

### Context

gh-aw compiles workflow Markdown, imported prompt fragments, and template conditionals into generated GitHub Actions prompt steps and lock files. Prompt expressions can be discovered in the main workflow and in ordered or legacy imports, across runtime-import, inline, and inlined-import modes. Previously, expression-extraction errors could be ignored, causing valid repository/run bindings to be discarded while compilation still emitted an artifact. At the same time, Markdown code examples may intentionally contain syntactically invalid implicit conditions and must remain literal without disabling validation of real conditions or explicit Actions expressions.

### Decision

We will treat prompt-expression extraction as a fail-closed compiler boundary for main Markdown and ordered or legacy imports in every prompt mode. Extraction errors will propagate before prompt steps or lock files are emitted, while valid expressions receive compiler-selected bindings. The runtime interpolator will resolve both `${GH_AW_EXPR_*}` and `__GH_AW_EXPR_*__` placeholders, validate selected author bindings, and insert evaluated values as literal data rather than recursively interpreting them. Only syntactically invalid implicit conditional examples inside code regions are exempted from wrapping; explicit Actions expressions and valid bindings in those regions remain subject to validation.

### Alternatives Considered

#### Alternative 1: Continue best-effort extraction

The compiler could continue to ignore extraction errors and emit the remaining mappings, preserving compilation for malformed prompts. This was rejected because a successful compilation could silently omit valid bindings and produce incomplete or incorrect prompts, which is the failure mode this change addresses.

#### Alternative 2: Defer validation to runtime

The compiler could emit prompt steps and let the runtime renderer or GitHub Actions report malformed expressions later. This was rejected because it moves a deterministic source error past lock-file generation and workflow publication, making failures later, less actionable, and harder to associate with the source Markdown.

#### Alternative 3: Reject every conditional-looking example in code regions

The compiler could validate and reject all template-like conditionals found in inline code and fenced code blocks. This was rejected because documentation and prompt content can legitimately show placeholder or malformed syntax as literal examples; only invalid implicit conditions in those regions should remain untouched, while explicit Actions expressions and valid bindings still require validation.

### Consequences

#### Positive

- Malformed active prompt conditions fail compilation before prompt steps or a new lock file are published.
- Valid expressions in main prompts and imports retain their bindings instead of being silently discarded.
- Compiler-to-runtime behavior is covered across runtime, inline, and inlined-import modes, including evaluated metadata and literal-value preservation.

#### Negative

- Existing workflows with malformed active conditions become compilation blockers and must be corrected before they can regenerate successfully.
- The compiler and runtime now share a more explicit binding contract, including placeholder compatibility, environment validation, and protection against recursive interpretation.
- Code-region detection adds parser maintenance and edge cases around Markdown delimiters and nested examples.

#### Neutral

- Runtime imports remain editable at execution time, but their available prompt expressions are analyzed during compilation so bindings can be generated and validated.
- Generated lock files change when compiler-selected bindings or runtime environment declarations change; unrelated regeneration drift is not part of this decision.
- Both placeholder forms remain supported for compatibility, with regression tests documenting the expected one-pass substitution behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
