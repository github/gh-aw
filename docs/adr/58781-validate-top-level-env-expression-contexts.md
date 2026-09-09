# ADR-58781: Validate top-level env expression contexts

**Date**: 2026-09-05
**Status**: Draft
**Deciders**: gh-aw maintainers

---

## Context

Top-level workflow `env` entries in GitHub Actions are evaluated at workflow scope, where only a limited set of contexts is available. This pull request fixes a real failure mode where expressions such as `${{ runner.temp }}` were accepted in top-level `env` even though `runner` is only available within jobs, causing generated workflows to fail at runtime. The compiler already validates other expression-safety rules, so this repository needs a deterministic validation step that rejects unavailable contexts both in local workflow frontmatter and in merged imported environment definitions. The implementation must avoid false positives for plain text and for quoted string literals inside expressions.

## Decision

We will add compiler-level validation for top-level `env` expressions that rejects contexts unavailable outside jobs: `env`, `job`, `jobs`, `matrix`, `needs`, `runner`, `steps`, and `strategy`. We will run this validation both during workflow expression validation and when merging imported environment definitions so that invalid workflow-scope environment variables fail compilation before runtime. We will implement the check by scanning `${{ ... }}` expressions, masking quoted literals, and producing deterministic errors that name the offending environment variable and instruct authors to move it to job- or step-level `env`.

## Alternatives Considered

### Allow invalid contexts and rely on GitHub Actions runtime errors

This matches the previous behavior and avoids adding compiler logic. It was rejected because the PR description and tests show that users can write top-level `env` expressions that compile successfully in `gh-aw` but fail later in GitHub Actions, making the tool less predictable and harder to debug.

### Restrict all top-level env expressions to plain literals only

A stricter option would be to ban expressions entirely in top-level `env`. It was rejected because the PR explicitly preserves valid workflow-scope contexts such as `github`, `inputs`, `vars`, and `secrets`, which remain useful and supported by GitHub Actions.

### Validate only direct workflow frontmatter and skip imported env merges

The compiler could have checked only the current workflow file's `env` block. It was rejected because this PR also merges imported environment definitions, and skipping merged env values would leave the same runtime failure mode reachable through imports.

## Consequences

### Positive

- Invalid workflow-scope `env` expressions now fail deterministically during compilation instead of causing runtime workflow failures in GitHub Actions.
- Imported and local environment definitions are validated consistently, reducing hidden differences between direct and merged configuration.

### Negative

- The compiler gains another specialized expression-validation rule that must be maintained alongside other GitHub Actions context rules.
- The regexp-based detection must be covered carefully to avoid regressions around bracket notation, property names, and quoted literals.

### Neutral

- Authors who need `runner` or other job-only contexts must move those variables into job or step `env` blocks instead of relying on top-level workflow `env`.
- New unit tests and compile-path tests document the supported and unsupported contexts for future contributors.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
