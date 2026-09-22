# ADR-62723: Fold shared import input fallbacks at compile time

**Date**: 2026-09-22
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request fixes how gh-aw compiles shared workflow imports when imported content uses fallback expressions such as `${{ github.aw.import-inputs.campaign || github.aw.import-inputs.package }}`. The PR description states that unresolved `github.aw.import-inputs.*` expressions were leaking into compiled workflows, which caused downstream admission checks to receive empty values instead of the selected import input. The implementation changes touch the parser paths that substitute import inputs before YAML parsing and add an integration test that compiles a sample imported workflow and checks the rendered lock output. The key design question is whether gh-aw should evaluate import-input-only fallback expressions during compilation rather than leaving them for GitHub Actions runtime.

### Decision

We will fold fallback expressions composed only of `github.aw.import-inputs.*` references during import substitution at compile time. gh-aw will apply this substitution consistently in both import parsing passes, using GitHub Actions-style truthiness so the compiler emits a literal selected value when one is available and avoids leaving `github.aw.import-inputs` expressions in compiled workflows. We chose this because imported workflow inputs are known during compilation, while the unresolved runtime context is invalid in generated workflows and breaks downstream consumers.

### Alternatives Considered

#### Alternative 1: Leave fallback resolution to GitHub Actions runtime

This matches the previous behavior of substituting only direct import-input references and leaving `||` expressions intact in the compiled workflow. It was not chosen because the PR evidence shows the emitted `github.aw.import-inputs.*` runtime context is invalid in the generated workflow and can produce empty values in admission checks.

#### Alternative 2: Require shared workflow authors to avoid fallback expressions in imports

This would avoid compiler changes by pushing the constraint onto imported workflow authors and template maintainers. It was not chosen because the PR description shows existing shared templates already rely on parameterized fallback expressions, and forbidding them would preserve a fragile authoring model instead of making compilation handle known import inputs correctly.

### Consequences

#### Positive
- Compiled workflows now emit concrete values for import-input fallback expressions when the selected import input is known at compile time.
- The compiler behaves consistently across both import parsing passes, reducing the chance that one import path leaves unresolved expressions behind.
- Integration coverage now checks end-to-end compilation for `campaign`, legacy `package`, and empty-input cases.

#### Negative
- The parser now implements a subset of GitHub Actions expression semantics, which increases maintenance burden if additional expression forms need to be supported later.
- Compile-time substitution logic becomes more coupled to truthiness behavior, so future changes must preserve compatibility with GitHub Actions expectations.
- The change adds parser complexity, including fallback-specific regex handling and value truthiness evaluation.

#### Neutral
- Fallback folding is limited to expressions composed entirely of `github.aw.import-inputs.*` operands joined by `||`; other expression shapes remain outside this behavior.
- Existing direct import-input substitution behavior remains in place alongside the new fallback handling.
- The new regression test validates compiled lock content rather than changing public CLI surface area.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
