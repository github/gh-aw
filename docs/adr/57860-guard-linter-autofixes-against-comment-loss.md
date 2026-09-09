# ADR-57860: Guard Linter Autofixes Against Comment Loss

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request changes five Go linters that currently emit whole-expression `SuggestedFix` text edits for simplifications such as `append`, `time.Now().Sub`, `strings.Join`, `strings.Count`, and `strings.Index`. The PR description and tests show that these fixes can silently delete inline or trailing comments when the replacement span overlaps commented source, because AST printing does not preserve comments inside the rewritten expression. The diff introduces a shared overlap check in `pkg/linters/internal/astutil` and updates each affected linter plus golden tests. Because this changes the policy for when autofixes are emitted across multiple analyzers, the behavior should be captured explicitly.

### Decision

We will keep reporting the diagnostics from these linters, but suppress their `SuggestedFix` autofixes whenever the replacement span overlaps source comments. We will centralize the overlap detection in `astutil.HasOverlappingComment` and make shared fix construction helpers accept the parsed files needed to perform that check. We chose this approach because preserving user comments is more important than always offering an automatic rewrite, and the diff shows a single reusable guard can address the issue consistently across all five linters.

### Alternatives Considered

#### Alternative 1: Continue Emitting Autofixes Unconditionally

Keep the existing behavior and allow the analyzers to replace the full expression even when comments appear inside the rewritten span.

This was considered because it preserves maximum autofix coverage and requires no additional guard logic. It was not chosen because the PR evidence shows this behavior can silently delete user comments, which is a correctness and trust issue for `-fix` output.

#### Alternative 2: Rebuild Fixes to Preserve Comments

Teach each linter to generate narrower edits or comment-aware rewrites that retain inline and trailing comments while still applying an autofix.

This was considered because it could preserve both automation and comments. It was not chosen in this PR because the implemented evidence shows a simpler, shared suppression strategy across five linters, while a fully comment-preserving rewrite mechanism would be more complex and is not demonstrated by the current changes.

### Consequences

#### Positive
- `go analysis -fix` no longer risks silently deleting overlapping inline or trailing comments for the affected linters.
- The overlap policy is applied consistently through a shared helper and shared test coverage.
- Diagnostics still surface the simplification opportunity even when an autofix is unsafe to apply automatically.

#### Negative
- Some findings that were previously auto-fixable now require manual edits when comments overlap the replacement span.
- Analyzer code paths become slightly more complex because fix generation now depends on parsed files and overlap checks.
- Future linters that emit whole-expression rewrites must remember to apply the same safety rule or use the shared helper correctly.

#### Neutral
- Testdata and golden files now explicitly encode the distinction between reporting a diagnostic and offering a fix.
- Shared helper signatures change to accept `pass.Files`, which updates caller contracts without changing the diagnostic messages themselves.
- The decision applies only to overlapping-comment cases; safe autofixes continue to be emitted for uncommented expressions.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
