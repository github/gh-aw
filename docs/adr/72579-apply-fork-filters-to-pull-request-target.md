# ADR-72579: Compile Fork Filters for Both PR Events, With an Asymmetric Default

**Date**: 2026-10-27
**Status**: Proposed
**Deciders**: Unknown (PR #72579)

---

### Context

gh-aw supports compiler-only `forks:` filters under both PR events and label-name filters (`names:`) under `on.pull_request`; these belong in job conditions, not native GitHub Actions trigger properties. The on-section cleanup state machine (`pkg/workflow/frontmatter_on_section_cleanup.go`) checked for the next event before leaving compiler-only subsections such as `on.permissions`, `on.steps`, or `on.skip-author-associations`, so filters after those fields could leak into lock files and fail Actions schema validation. Separately, `applyPullRequestForkFilter` (`pkg/workflow/filters.go`) read only `on.pull_request`, silently ignoring explicit `pull_request_target.forks` restrictions. Correct compilation must enforce configured fork admission without changing the existing behavior of workflows that omit the field.

### Decision

The compiler will apply explicit fork filters to both `pull_request` and `pull_request_target` using event-scoped job conditions (`github.event_name != '<event>' || ...`), while preserving existing conditions and commenting out processed trigger fields. For configured event objects, omitting `forks` retains the same-repository-only default for `pull_request` and adds no fork restriction for `pull_request_target`; explicit patterns always admit same-repository PRs, `[]` admits only those PRs, and `*` adds no fork restriction. The supported frontmatter schema in `pkg/parser/schemas/main_workflow_schema.json` accepts string and list patterns for both events, but accepts the `forks: {author-association: [...]}` form from [ADR-69374](69374-fork-pr-admission-by-author-association.md) only for `pull_request`; the shared helper's ability to process that object does not make it supported for `pull_request_target`, and this decision does not extend the schema. To produce valid lock files without silently dropping configured admission rules, the cleanup state machine will leave compiler-only subsections before detecting the next event and track `forks` indentation for nested content, including the supported two-space and four-space event layouts.

### Alternatives Considered

#### Alternative 1: Reject `forks:` under `pull_request_target` at schema/compile time

The compiler could treat `forks` as a `pull_request`-only key and raise an error when it appears under `pull_request_target`. That would also stop the silent drop. It was rejected because the field is documented and useful: authors of `pull_request_target` workflows sometimes want to admit only specific fork owners. An error would take away that control instead of providing it.

#### Alternative 2: Apply the same same-repository default to `pull_request_target`

Using one rule for both events would be simpler: omitting `forks` would block forks everywhere. It was rejected because it would change the behavior of existing `pull_request_target` workflows that admit fork PRs, even when their authors had not configured a fork restriction. The asymmetric default preserves existing fork-admission behavior unless the author opts in.

#### Alternative 3: Fix only the cleanup leak and keep `pull_request`-only fork filtering

A narrower bug fix would remove the leaked keys and leave admission logic alone. It was rejected because removing `forks` from `pull_request_target` without compiling it into a condition would make the silent drop permanent. The author would get a lock file that looks valid but does not enforce the restriction they configured.

### Consequences

#### Positive
- Recognizing PR events after compiler-only `on.*` fields prevents `forks`/`names` from leaking into native triggers and failing Actions schema validation.
- An explicit `forks:` pattern under `pull_request_target` is now enforced as an event-scoped job condition instead of being ignored.
- Event-scoped guards let workflows that set both PR events use independent fork filters, and existing `if:` and label (`names`) conditions are kept.

#### Negative
- The two PR events now have different defaults for an omitted `forks`, so authors must remember that only `pull_request` blocks forks by default.
- Recompiling workflows that already had a `forks:` key under `pull_request_target` will start enforcing it. This could stop runs that previously, and unintentionally, admitted all forks.
- The line-oriented cleanup state machine depends on event-ordering logic and indent tracking, which remain fragile compared with editing the YAML AST.
- The author-association object form remains unavailable for `pull_request_target`, despite the shared helper accepting it internally.

#### Neutral
- The [trigger reference](../src/content/docs/reference/triggers.md) describes the `pull_request_target` pattern-filter semantics explicitly.
- Regression tests in `pkg/workflow/filters_forks_test.go` and `pkg/workflow/frontmatter_on_section_cleanup_test.go` cover fork conditions, generated Actions schema validation, compiler-only field ordering, and two-space/four-space event indentation.
- Fork admission does not grant permissions or secrets and does not make checking out or executing untrusted fork code safe.
