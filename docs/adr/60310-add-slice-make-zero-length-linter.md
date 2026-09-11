# ADR-60310: Add Slice Make Zero Length Linter

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new custom Go analyzer under `pkg/linters/` and registers it in the shared linter registry, which makes the change part of the repository's standard static-analysis policy rather than an isolated utility. The implementation targets calls of the form `make([]T, 0)` without a capacity argument and treats them as a performance-oriented code smell, with tests showing both expected findings and allowed cases such as explicit capacity, non-zero length, and `nolint` suppression. Because the linter becomes part of the central analyzer suite, the architectural decision is whether this repository should enforce this allocation pattern through automated linting. The available PR evidence emphasizes performance and repeated review feedback as the primary drivers for codifying the rule.

### Decision

We will add a custom `slicemakezerolength` analyzer to the repository's shared linter registry to flag `make([]T, 0)` calls that omit a capacity argument. We decided to encode this performance recommendation as a reusable static-analysis rule, with support for existing repository conventions such as generated-file skipping, coverage gating, and `nolint` suppression. This favors consistent automated enforcement of a repeated code-review concern over relying on manual review comments.

### Alternatives Considered

#### Alternative 1: Keep this as a code review guideline only

The team could continue treating `make([]T, 0)` without capacity as an informal review suggestion instead of building a dedicated analyzer. This was considered because the PR body explicitly describes the pattern as a common review comment, and manual review avoids growing the custom linter suite. It was not chosen because the diff shows the pattern occurs in multiple locations across the codebase and the change aims to make the guidance consistent and automatically enforceable.

#### Alternative 2: Rely on existing third-party linters

Another option would be to depend on an upstream linter or broader performance lint package instead of adding a repository-specific analyzer. This was considered because it could reduce local maintenance and reuse community tooling. It was not chosen because the PR implements the rule directly in `pkg/linters/`, integrates it with the local registry and helper utilities, and therefore indicates the repository wants targeted behavior aligned with its existing custom-linter framework.

#### Alternative 3: Broaden the rule to infer final slice size before reporting

The analyzer could attempt deeper data-flow analysis and only report cases where the final capacity can be proven from surrounding code. This was considered because the PR description frames the issue in terms of known or estimable final lengths. It was not chosen because the actual implementation intentionally uses a simpler syntactic rule—reporting `make([]T, 0)` without a capacity argument—trading precision for low complexity and predictable enforcement.

### Consequences

#### Positive
- The repository will enforce this slice-allocation convention consistently across reviews and automation.
- Developers get fast feedback for a repeated performance-oriented pattern without waiting for reviewer intervention.
- The analyzer fits the existing custom linter architecture, including registry-based activation, testdata-driven verification, coverage gating, and `nolint` support.

#### Negative
- The rule may report some cases where omitted capacity is harmless or where the final size is not actually inferable, creating false positives relative to the PR's stated motivation.
- Maintaining another custom analyzer increases long-term cost for compatibility, testing, and linter-suite complexity.
- Codifying this recommendation as a lint rule may push style and micro-optimization policy into CI, which can increase friction for contributors.

#### Neutral
- The implementation only adds diagnostics; it does not include an automatic fix or rewrite.
- Existing suppression mechanisms remain available through `//nolint:slicemakezerolength`.
- The decision extends the current custom-linter framework rather than introducing a new enforcement mechanism.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
