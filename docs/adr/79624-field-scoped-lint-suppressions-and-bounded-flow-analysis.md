# ADR-79624: Field-Scoped Lint Suppression Comments and Bounded Static Flow Analysis for Security Lint Gaps

**Date**: 2026-11-21
**Status**: Draft
**Deciders**: [TODO: verify] PR #79624 author (pelikhan); `pkg/workflow/lint` maintainers to confirm

---

### Context

The Actions/AW workflow linter in `pkg/workflow/lint` had verified gaps: cross-run artifact execution through Node preloads/imports/loaders, executable inputs of reviewed third-party actions, attacker-controlled data transported through shell step and job outputs, writes to `GITHUB_ENV`/`GITHUB_PATH`, and unpinned container/service images. Closing them adds conservative taint propagation, which also adds false positives (Node metadata commands, literal `'$GITHUB_ENV'` filenames, disabled services, excluded matrix images). Operators had no narrow, auditable way to acknowledge an incorrect diagnostic, and the compiler must stay offline and deterministic and must still reject confirmed errors.

### Decision

We will extend the existing symbolic shell/taint model (`pkg/workflow/taint`) with new rules (`environment-file-injection`, `unpinned-images`) and wider `template-injection` and `artifact-execution` coverage, rather than adding a new analysis engine. We will add field-scoped `# gh-aw: ignore[rule-id, ...] -- reason` YAML comments that require exact registered rule IDs and a nonempty reason, reject malformed or misplaced directives, and keep sibling diagnostics, analysis errors, strict-mode checks, and runtime protections independent of them. Unsupported shells and unresolved flows produce coverage warnings that the compiler omits from its warning count while still failing on confirmed errors. The primary driver is closing real gaps while keeping false positives correctable without weakening security controls.

### Alternatives Considered

#### Alternative 1: File-wide or disable/enable suppression directives

A directive such as `# gh-aw: disable` or a per-file ignore list would be simpler to implement and familiar from other linters. It was rejected because it hides unrelated and future findings in the same file, and acknowledgements could not be tied to a single field and reason. Field scoping with a mandatory reason keeps each exception reviewable.

#### Alternative 2: Fix false positives only in rule logic, with no suppression mechanism

Tuning rules until no false positives remain avoids a new user-facing syntax. It was rejected because the analysis is intentionally conservative and bounded (for example, arbitrary shell validation is not treated as sanitization), so some incorrect diagnostics will remain and operators would have no documented remedy short of disabling rules or skipping lint.

#### Alternative 3: Full interprocedural or interpreter-aware data-flow analysis

Modeling JavaScript, PowerShell, action internals, and cross-job artifact flows would reduce the unsupported cases. It was rejected because it needs network access or code execution to resolve actions, and conflicts with the offline, deterministic compiler. Bounded checks with explicit coverage warnings were chosen instead.

### Consequences

#### Positive
- Cross-run artifact execution, executable action inputs, output-transported injection, runner environment-file injection, and unpinned images are now detected.
- Known false positives are corrected, and the remaining ones can be acknowledged per field with an auditable reason.
- Suppressions do not disable compiler validation, strict-mode controls, or runtime protections, so a comment cannot silently weaken security.
- The compiler stays offline and deterministic, and confirmed errors still fail compilation.

#### Negative
- A new comment syntax must be documented, parsed, and maintained, and malformed directives now fail linting.
- Conservative taint propagation can still produce false positives and misses, since unsupported interpreters and unresolved flows are only warned about.
- AW comments are not copied into generated Actions graphs, so compiled execution diagnostics need directives in the Actions YAML and AW-only rules need them in frontmatter; anchors do not carry suppressions to aliases.
- Omitting flow-coverage warnings from the compiler warning count lowers their visibility there; `gh aw lint` retains them.

#### Neutral
- Rule IDs become a public contract for suppression directives, so renaming a rule is a breaking change.
- New rules are registered in the default registry and documented in `docs/src/content/docs/reference/workflow-linters.md`.
- Two new skills (`retrieve-public-aw-workflows`, `top-public-aw-repositories`) ride along in this PR and are unrelated to this decision.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
