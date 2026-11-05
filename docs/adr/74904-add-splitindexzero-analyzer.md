# ADR-74904: Add splitindexzero Analyzer

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

The gh-aw repository maintains custom Go analysis linters under `pkg/linters` for patterns that are not covered by the default linting configuration. The `strings.Split(s, sep)[0]` and `strings.SplitN(s, sep, 2)[0]` patterns allocate a slice when callers need only the first segment, while `strings.Cut` avoids that allocation. The codebase has approximately eight existing instances of this pattern, and no existing custom or default golangci-lint rule covers it. The new check therefore needs to identify the standard-library `strings` package accurately while allowing existing violations to be remediated before CI enforcement.

### Decision

We will add a custom `splitindexzero` analyzer to `pkg/linters`, register it with the analyzer registry, and document it with the other linters. The analyzer will report index-zero reads from `strings.Split` with a constant non-empty separator and `strings.SplitN` with a constant non-empty separator and limit of 2, resolving the standard-library package by type and ignoring aliases, shadowed names, generated files, and `nolint`-suppressed findings. We will register the analyzer as not yet enforced so existing violations can be addressed before it becomes a CI gate.

### Alternatives Considered

#### Alternative 1: Extend an existing linter or rely on the default golangci-lint rules

This would avoid adding another analyzer package, but the issue evidence indicates that no existing custom analyzer or default golangci-lint rule covers this specific allocation pattern. Extending an unrelated analyzer would also make its scope and documentation less clear.

#### Alternative 2: Enable CI enforcement immediately

Immediate enforcement would provide the strongest guarantee that new violations do not land. It was not chosen because the repository already contains approximately eight findings; adding the check as a required gate before those are remediated would fail existing code rather than support a staged rollout.

#### Alternative 3: Rely on code review and manual remediation

Manual review would avoid analyzer maintenance, but it would be inconsistent and would not reliably catch aliases, shadowing, generated files, or suppression directives. It also provides no repeatable signal for future changes.

### Consequences

#### Positive
- The repository can detect a specific avoidable allocation pattern with type-aware matching and predictable exclusions.
- Developers receive a direct recommendation to use `strings.Cut`, and the analyzer is integrated into the registry, documentation, and analyzer tests.

#### Negative
- The repository gains another analyzer to maintain, document, and keep aligned with shared coverage, generated-file, and `nolint` handling.
- The analyzer is initially not CI-enforced, so existing and newly introduced violations can remain until a follow-up remediation enables enforcement.

#### Neutral
- The implementation is limited to constant non-empty separators and the `SplitN` limit of 2; dynamic or semantically different cases remain outside its scope.
- Generated files and explicitly suppressed findings are intentionally excluded, and hot-path coverage gating continues to determine whether a finding is reported.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
