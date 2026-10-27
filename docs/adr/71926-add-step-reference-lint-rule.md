# ADR-71926: Add a Default `step-reference` Lint Rule for Dangling Step References

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR author) [TODO: verify]

---

### Context

Generated conclusion jobs referenced reporting steps (for example `steps.report_incomplete.outputs.incomplete_count`) that were absent from work-queue jobs, a bug fixed in #71898. GitHub Actions evaluates a reference to a missing step as an empty value rather than failing, so these dangling references silently produce wrong job outputs and conditions. The existing lint suite (`pkg/workflow/lint`) already validated cross-job `needs.*` references (`needs-consistency`) but had no equivalent check for intra-job `steps.*` references. AW (authored markdown) workflows can also acquire steps from imports, compiler-generated jobs, and `restore-memory`, so authored source is not always a complete picture of a job's steps.

### Decision

We will add a new built-in, default-enabled `step-reference` lint rule (Warning severity, targeting both Actions and AW ASTs) that parses expressions with actionlint's expression parser and reports literal `steps.<id>` / `steps['<id>']` accesses whose ID is not declared in the same job (case-insensitive). The compiled Actions graph is treated as authoritative when available; AW-only analysis checks only scopes whose steps are fully visible in authored source (custom `jobs`, `safe-jobs`, `safe-outputs.jobs`, including setup/pre/post steps) and defers ambiguous scopes (unresolved imports, compiler-owned jobs, `restore-memory`, dynamic step IDs). The rule validates step existence only — not output names, context availability, or step ordering — to keep false positives near zero.

### Alternatives Considered

#### Alternative 1: Rely on actionlint's built-in checks

actionlint already type-checks `steps` context accesses in plain Actions workflows. It was not chosen as the sole mechanism because it does not understand AW-specific scopes (`safe-jobs`, `safe-outputs.jobs`, `setup-steps`/`pre-steps`), and the gh-aw lint framework needs a rule ID, severity, and path-based diagnostics consistent with other built-in rules such as `needs-consistency`.

#### Alternative 2: Stricter validation (outputs and ordering) as an error

The rule could also validate that referenced outputs are declared and that producer steps run before consumers, and fail compilation. This was rejected because external actions can emit arbitrary outputs that cannot be known statically, and a hard error on ambiguous AW scopes would block valid workflows; a warning restricted to step existence catches the observed bug class with low false-positive risk.

#### Alternative 3: Fix generator-side only (no lint)

Correcting the generator (as done in #71898) without adding a lint would leave regressions undetected. A lint rule turns this bug class into a continuously enforced check across both compiled and authored workflows.

### Consequences

#### Positive
- Dangling step references in generated and authored workflows are surfaced as warnings at compile/lint time instead of silently evaluating to empty values at runtime.
- Reuses existing infrastructure (actionlint expression AST, `walkExpressionScalars`, `taint.VisitInterpolations`, `aw.ExecutionBlocks`) and mirrors the established `needs-consistency` design.

#### Negative
- AW-only analysis intentionally skips root steps, `safe-outputs` steps, compiler-owned jobs, `restore-memory` jobs, and workflows with pending imports or dynamic step IDs, so some dangling references are only caught once the compiled graph is linted.
- Adds another default rule to maintain; changes to how the compiler emits setup/pre-steps or compiler-owned job names must be kept in sync with the rule's scope logic.

#### Neutral
- Severity is Warning in either strict mode, so existing workflows will not fail to compile but may show new diagnostics.
- At most one diagnostic is reported per expression path, and output names are deliberately not validated.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
