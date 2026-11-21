# ADR-79624: Field-Scoped Lint Suppression Comments and Bounded Static Flow Analysis for Security Lint Gaps

**Date**: 2026-11-21
**Status**: Proposed
**Deciders**: Not recorded; maintainer review and approval are pending

---

### Context

The Actions/AW workflow linter in `pkg/workflow/lint` had verified gaps: cross-run artifact execution through Node preloads/imports/loaders, executable inputs of reviewed third-party actions, attacker-controlled data transported through shell step and job outputs, writes to `GITHUB_ENV`/`GITHUB_PATH`, and unpinned container/service images. Closing them adds conservative taint propagation and requires distinguishing false positives such as Node metadata commands, literal `'$GITHUB_ENV'` filenames, disabled services, and excluded matrix images. Operators had no narrow, auditable way to acknowledge an incorrect diagnostic. Lint analysis must remain offline and deterministic, and unsuppressed error diagnostics must still fail compilation.

### Decision

The proposal extends the existing symbolic shell/taint model (`pkg/workflow/taint`) with new rules (`environment-file-injection`, `unpinned-images`) and wider `template-injection` and `artifact-execution` coverage, rather than adding a new analysis engine. It adds standalone `# gh-aw: ignore[rule-id, ...] -- reason` YAML comments immediately before a field at matching indentation. Directives require exact registered rule IDs and a nonempty reason; malformed or misplaced directives fail linting. Suppression applies to the named rules at that field and its descendants, not sibling fields, and does not suppress analysis errors, independent strict-mode checks, or runtime protections.

Analysis remains bounded to supported syntax and modeled flows, without fetching or executing action internals. Known gaps receive rule-specific diagnostics; selected environment-file and output-flow coverage warnings remain visible in `gh aw lint` but are omitted from the compiler warning count. This is not a blanket downgrade of unresolved inputs. Unsuppressed errors still fail compilation. Suppressions make false positives reviewable, but their reasons are acknowledgements, not proof that a finding is safe.

### Alternatives Considered

#### Alternative 1: File-wide or disable/enable suppression directives

A directive such as `# gh-aw: disable` or a per-file ignore list would be simpler to implement and familiar from other linters. It was rejected because it hides unrelated and future findings in the same file, and acknowledgements could not be tied to a single field and reason. Field scoping with a mandatory reason keeps each exception reviewable.

#### Alternative 2: Fix false positives only in rule logic, with no suppression mechanism

Tuning rules until no false positives remain avoids a new user-facing syntax. It was rejected because the analysis is intentionally conservative and bounded (for example, arbitrary shell validation is not treated as sanitization), so some incorrect diagnostics will remain and operators would have no documented remedy short of disabling rules or skipping lint.

#### Alternative 3: Full interprocedural or interpreter-aware data-flow analysis

Richer JavaScript, PowerShell, action-internal, and cross-job models could reduce unsupported cases. Static analysis can remain offline, but deeper models require additional semantics, action source inputs, and maintenance beyond the current lint contract. Fetching or executing action internals during linting is outside that contract. Extending the existing bounded model with explicit gap diagnostics keeps this change scoped without claiming complete flow analysis.

### Consequences

#### Positive
- Supported cross-run artifact execution, executable action inputs, output-transported injection, runner environment-file injection, and unpinned images are diagnosed.
- Known false positives are corrected, and the remaining ones can be acknowledged per field with an auditable reason.
- Suppressions do not disable independent compiler validation, strict-mode controls, or runtime protections.
- Lint analysis stays offline and deterministic, and unsuppressed errors still fail compilation.

#### Negative
- A new comment syntax must be documented, parsed, and maintained, and malformed directives now fail linting.
- Conservative taint propagation can still produce false positives and misses. Gap diagnostics do not prove that an unsupported interpreter or unresolved flow is safe.
- A suppression can hide a real finding for its named rules, including findings under a mapping field; reviewers must assess the reason and prefer the narrowest field.
- AW comments are not copied into generated Actions graphs, so compiled execution diagnostics need directives in the Actions YAML and AW-only rules need them in frontmatter; anchors do not carry suppressions to aliases.
- Omitting flow-coverage warnings from the compiler warning count lowers their visibility there; `gh aw lint` retains them.

#### Neutral
- Rule IDs become a public contract for suppression directives, so renaming a rule is a breaking change.
- New rules are registered in the default registry and documented in `docs/src/content/docs/reference/workflow-linters.md`.
- The `retrieve-public-aw-workflows` and `top-public-aw-repositories` skills support the requested public-workflow discovery follow-up. They are separate operational guidance, not part of this lint architecture.

---

*Proposed for maintainer review in [PR #79624](https://github.com/github/gh-aw/pull/79624). No maintainer approval is recorded here; change to Accepted only after approval and record the deciders then.*
