# ADR-72579: Compile Fork Filters for Both PR Events, With an Asymmetric Default

**Date**: 2026-07-22
**Status**: Draft
**Deciders**: Unknown (PR #72579)

---

### Context

gh-aw lets workflow authors write compiler-only filters such as `forks:` and `names:` under `on.pull_request` / `on.pull_request_target`. GitHub Actions does not accept these keys, so the compiler must convert them into job `if:` conditions and comment them out of the generated `on:` block. Two problems showed up. First, the on-section cleanup state machine (`pkg/workflow/frontmatter_on_section_cleanup.go`) left compiler-only subsections such as `on.permissions`, `on.steps`, or `on.skip-author-associations` only *after* checking for the next event. When one of those fields came before a PR event, `forks`/`names` leaked into the lock file, and `gh aw compile --validate` then rejected it. Second, `applyPullRequestForkFilter` (`pkg/workflow/filters.go`) only read `on.pull_request`. An explicit `forks:` under `pull_request_target` was silently ignored and was never turned into an admission check.

### Decision

We will handle fork filters the same way for `pull_request` and `pull_request_target`. Each event gets its own job condition, guarded by `github.event_name != '<event>' || ...`, and the processed `forks` field is removed from the trigger for both events. The defaults stay different on purpose: `pull_request` keeps its existing same-repository-only default when `forks` is omitted, while `pull_request_target` gets no fork restriction unless `forks` is set explicitly. We will also fix the cleanup state machine so it leaves compiler-only subsections *before* it detects the next event section. It now records `forks` indentation, which lets it handle multiline lists at any indent. The main driver is correctness: compiled lock files must pass GitHub Actions schema validation, and a fork restriction the author wrote down must never be silently dropped.

### Alternatives Considered

#### Alternative 1: Reject `forks:` under `pull_request_target` at schema/compile time

The compiler could treat `forks` as a `pull_request`-only key and raise an error when it appears under `pull_request_target`. That would also stop the silent drop. It was rejected because the field is documented and useful: authors of `pull_request_target` workflows sometimes want to admit only specific fork owners. An error would take away that control instead of providing it.

#### Alternative 2: Apply the same same-repository default to `pull_request_target`

Using one rule for both events would be simpler: omitting `forks` would block forks everywhere. It was rejected because it would quietly change the behavior of existing `pull_request_target` workflows. Many of them exist specifically to handle fork PRs, so recompiling would stop them from running on forks. The asymmetric default keeps existing behavior unless the author opts in.

#### Alternative 3: Fix only the cleanup leak and keep `pull_request`-only fork filtering

A narrower bug fix would remove the leaked keys and leave admission logic alone. It was rejected because removing `forks` from `pull_request_target` without compiling it into a condition would make the silent drop permanent. The author would get a lock file that looks valid but does not enforce the restriction they configured.

### Consequences

#### Positive
- Lock files no longer contain `forks`/`names` trigger properties, so `compile --validate` succeeds regardless of where compiler-only `on.*` fields appear.
- An explicit `forks:` pattern under `pull_request_target` is now enforced as an event-scoped job condition instead of being ignored.
- Event-scoped guards let workflows that set both PR events use independent fork filters, and existing `if:` and label (`names`) conditions are kept.

#### Negative
- The two PR events now have different defaults for an omitted `forks`, so authors must remember that only `pull_request` blocks forks by default.
- Recompiling workflows that already had a `forks:` key under `pull_request_target` will start enforcing it. This could stop runs that previously, and unintentionally, admitted all forks.
- The line-oriented cleanup state machine depends on event-ordering logic and indent tracking, which remain fragile compared with editing the YAML AST.

#### Neutral
- Documentation (`docs/src/content/docs/reference/triggers.md`) now describes the `pull_request_target` semantics explicitly.
- Regression tests cover wildcard, empty, exact, multiline, and mixed patterns, with both two-space and four-space indentation.
- This builds on the fork-admission model in [ADR-69374](69374-fork-pr-admission-by-author-association.md). Whether the author-association object form applies to `pull_request_target` is [TODO: verify].

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
