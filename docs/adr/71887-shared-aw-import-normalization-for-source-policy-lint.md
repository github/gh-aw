# ADR-71887: Shared AW Import Normalization for Source Policy Lint Rules

**Date**: 2026-05-15
**Status**: Draft
**Deciders**: Unknown (PR #71887 author and reviewers) [TODO: verify]

---

### Context

The source policy lint rules in `pkg/workflow/lint/source_policy.go` need to know which AW imports an authored workflow declares. Imports can be written as a top-level list (`imports: [...]`) or as a mapping with an `aw` key (`imports: { aw: [...] }`). Lists can arrive as `[]any` or `[]string`, depending on how the frontmatter was decoded. Before this PR, two call sites handled these shapes separately: `authoredImportsPending`, which defers authored-policy rules when configuration may be inherited, and `checkConditionalImports`. Each had its own type switch, which risked inconsistent validation as the supported import forms change (issue #71499).

### Decision

We will put AW import-shape resolution in one helper, `authoredImportEntries(frontmatter)`. It resolves `imports` or `imports.aw`, turns `[]any` and `[]string` into `[]any`, and returns `(entries, path, present, err)`. A null list returns the sentinel `errNullImportList`, and a non-list value returns a shape error. Each caller still produces its own diagnostics. `authoredImportsPending` keeps treating a top-level `imports: null` as "nothing inherited" while rejecting `imports.aw: null`. `checkConditionalImports` keeps its message "conditional-import requires <path> to be a list". The helper only normalizes shape. Policy decisions stay with the callers.

### Alternatives Considered

#### Alternative 1: Keep per-rule inline type switches

Each rule would keep parsing `imports` on its own. This keeps every rule self-contained and lets each one tune its edge cases independently. We rejected it because it is the duplication issue #71499 flagged: when a new import form is added, every switch must be updated together, or the rules start disagreeing about what counts as a valid import list.

#### Alternative 2: Normalize imports once into a typed model upstream (parser/Document level)

The frontmatter parser would turn `imports` into a typed struct, such as `Document.AWImports`, so lint rules never see raw `map[string]any` values. This gives stronger typing and a single source of truth for the whole codebase. We did not choose it here because it would widen the change beyond the lint package and touch the parser contract. It would also make it harder to keep the caller-specific handling of `imports: null` that the rules rely on today.

#### Alternative 3: Helper returns a fully decided result (pending / error message)

The helper would also decide whether imports are pending and build the user-facing errors. We rejected this because the two callers need different diagnostics and handle top-level null differently. Folding those decisions into the helper would mean either losing that distinction or adding mode flags.

### Consequences

#### Positive
- There is a single place that defines which import shapes are accepted (`imports` list, `imports.aw` list, `[]any`/`[]string`). Rules that consume it can no longer drift apart.
- A table-driven test (`TestAuthoredImportEntries`) now covers the shape matrix, including absent/null/empty/malformed cases, and checks that error messages do not leak raw values.
- New source-policy rules can reuse the helper instead of writing another type switch.

#### Negative
- The helper returns four values, and callers must interpret `present` together with `err`. This makes misuse easier, for example checking `present` before `err`.
- The caller-specific null handling now depends on matching the `errNullImportList` sentinel and comparing `path == "imports"`. This is an implicit contract that future edits could break.
- For `[]string` input, the helper allocates a new `[]any` copy on each call.

#### Neutral
- User-facing diagnostic texts are intentionally unchanged, so existing tests and users see the same messages.
- The helper does not validate individual entries (for example, `[]any{42}` passes through). Entry-level checks remain the responsibility of each rule.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
