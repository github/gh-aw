# ADR-60026: Add Buffer Reset Before Reuse Linter

**Date**: 2026-09-10
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analysis linter under `pkg/linters/bufferresetbeforereuse/` and registers it in the shared linter registry. The implementation scans function bodies for `bytes.Buffer` and `strings.Builder` variables that are written to, read from, and then written to again without an intervening `Reset()`. The PR includes analyzer test coverage and test fixtures that show both accepted and rejected usage patterns, including support for pointer buffers and `nolint` suppression. The architectural question is whether this buffer-reuse bug pattern should be enforced as a first-class repository linter rather than left to code review or broader existing lint rules.

### Decision

We will add a dedicated `bufferresetbeforereuse` analyzer to the repository's linter suite and register it in the global analyzer registry. We decided to model this as a custom AST and type-based linter because the PR evidence shows a specific correctness bug pattern involving stateful reuse of `bytes.Buffer` and `strings.Builder` that is not covered by the existing standard lint set. This makes the rule reusable across the codebase, testable with fixture-based cases, and suppressible through the existing `nolint` mechanism when a caller intentionally accepts the pattern.

### Alternatives Considered

#### Alternative 1: Rely on manual code review for buffer and builder reuse

The team could continue to detect improper `bytes.Buffer` and `strings.Builder` reuse during human review. This was considered because the misuse pattern is understandable to experienced Go reviewers and does not require new analyzer infrastructure. It was not chosen because the PR explicitly introduces automated detection, test fixtures, and registry integration, indicating that the bug is subtle enough to escape review and valuable enough to check systematically.

#### Alternative 2: Depend only on existing general-purpose Go linters

Another option would be to keep the current linter set unchanged and assume existing upstream analyzers are sufficient. This was considered because adding a custom analyzer increases maintenance cost and the repository already ships many lint rules. It was not chosen because the PR description and implementation are centered on a repository-specific gap: writes after reads on buffers/builders without `Reset()` are treated as a correctness hazard that standard lint tooling does not already flag.

#### Alternative 3: Detect the pattern with simpler text or grep-based heuristics

The project could try to catch buffer reuse with lightweight string matching on method calls rather than AST and type inspection. This was considered because it would be simpler to implement initially. It was not chosen because the analyzer needs to distinguish actual `bytes.Buffer` and `strings.Builder` variables, support both value and pointer forms, and respect existing `nolint` and generated-file behavior, which are better served by structured analysis.

### Consequences

#### Positive
- The repository gains automated detection of a concrete stale-data bug pattern involving `bytes.Buffer` and `strings.Builder` reuse.
- The rule is centralized in the linter registry, so the check can run consistently across the codebase rather than depending on reviewer memory.
- Fixture-based tests document expected behavior for valid resets, invalid reuse, pointer buffers, and suppression directives.

#### Negative
- The project takes on long-term maintenance for another custom analyzer, including updates if supported write/read APIs or repository linter infrastructure evolve.
- A block-level event model may still miss some control-flow-sensitive cases or produce behavior that needs future refinement as real code patterns appear.
- Contributors now face another lint rule that may require code changes or explicit suppression in edge cases.

#### Neutral
- The implementation follows the repository's existing analyzer utility, generated-file skipping, logging, and `nolint` integration patterns.
- The rule is added as a new package plus registry wiring, without changing the broader linter execution architecture.
- The PR expands testdata-based analyzer coverage alongside the production linter code.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
