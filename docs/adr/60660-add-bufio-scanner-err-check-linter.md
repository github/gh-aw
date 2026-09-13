# ADR-60660: Add Bufio Scanner Err Check Linter

**Date**: 2026-09-13
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analysis linter to `pkg/linters/` and registers it in the shared analyzer registry. The PR description states that unchecked `bufio.Scanner` loops silently drop I/O errors and reports nine existing matches in the repository, so the change is introducing a new static-analysis rule rather than fixing one isolated call site. The design choice in scope is whether gh-aw should encode this bug pattern as a dedicated analyzer with repository-specific test coverage and standard linter integration. The implementation also needs to respect existing analyzer conventions such as generated-file skipping and `nolint` directives.

### Decision

We will add a dedicated `bufioscannererunchecked` analyzer to the gh-aw linter suite to detect `bufio.Scanner` loops that do not check `scanner.Err()` after iteration completes. We decided to integrate it through the existing analyzer registry and test it with positive and negative fixtures so the rule becomes part of the normal linter toolchain. This was chosen because the PR evidence shows a concrete, recurring bug pattern with straightforward remediation and enough in-repo signal to justify a first-class analyzer.

### Alternatives Considered

#### Alternative 1: Rely on manual code review and ad hoc bug fixes

The project could leave this pattern to reviewers and fix unchecked scanner loops only when individual bugs are noticed. This was considered because it avoids adding another analyzer and any associated maintenance cost. It was not chosen because the PR description identifies nine existing instances, which suggests review alone is not consistently catching the issue.

#### Alternative 2: Extend an existing linter instead of adding a dedicated analyzer

Another option would be to fold the check into some broader error-handling or loop-analysis linter. This was considered because it could reduce the number of individual analyzers in the registry. It was not chosen because the diff creates a focused package, dedicated fixtures, and explicit registry entry, indicating the pattern is specific enough to warrant an isolated analyzer with its own behavior and tests.

#### Alternative 3: Fix the currently known scanner sites without adding enforcement

The team could patch the reported call sites and stop there without codifying the rule. This was considered because it would remove the immediate defects with less infrastructure change. It was not chosen because the PR is explicitly about adding reusable detection, which better prevents regressions than one-time fixes.

### Consequences

#### Positive
- The repository gains automated detection for a real error-handling bug pattern that can silently hide read failures.
- The rule is integrated into the existing linter registry, making enforcement consistent across normal analyzer runs.
- Dedicated `bad` and `good` fixtures document the intended behavior and reduce the chance of accidental regressions in the analyzer.

#### Negative
- The codebase takes on another custom analyzer that must be maintained as Go AST patterns and internal helper APIs evolve.
- False positives or missed edge cases remain possible because the analyzer infers control-flow patterns from syntax and type information.
- Contributors may need to learn and occasionally suppress the new rule when a special-case pattern is intentional.

#### Neutral
- The implementation follows existing analyzer conventions by consulting generated-file and `nolint` indexes before reporting diagnostics.
- The public architectural effect is additive: one new linter package and one new registry entry, without changing the linter execution model.
- Future fixes for the nine reported matches can proceed independently of this ADR because this PR focuses on detection and registration.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
