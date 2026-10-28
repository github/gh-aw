# ADR-72579: Compile PR Fork Filters and Require Bounded Target Admission

**Date**: 2026-10-27
**Status**: Proposed
**Deciders**: Unknown (PR #72579)

---

### Context

gh-aw supports compiler-only `forks:` filters under both PR events and label-name filters (`names:`) under `on.pull_request`; these belong in job conditions, not native GitHub Actions trigger properties. The on-section cleanup state machine (`pkg/workflow/frontmatter_on_section_cleanup.go`) checked for the next event before leaving compiler-only subsections such as `on.permissions`, `on.steps`, or `on.skip-author-associations`, so filters after those fields could leak into lock files and fail Actions schema validation. Separately, `applyPullRequestForkFilter` (`pkg/workflow/filters.go`) read only `on.pull_request`, silently ignoring explicit `pull_request_target.forks` restrictions.

Security review also identified that omitted target fork filters and `"*"` both admit arbitrary forks. Trusted actor authorization is insufficient: a maintainer can label an attacker-controlled PR, and an agent can consume malicious PR content with privileged capabilities even when checkout is disabled. Strict mode therefore needs explicit bounded admission, independently of actor authorization, checkout policy, and risk acknowledgment.

### Decision

The compiler will apply explicit fork filters to both `pull_request` and `pull_request_target` using event-scoped job conditions (`github.event_name != '<event>' || ...`), while preserving existing conditions and commenting out processed trigger fields. Explicit patterns always admit same-repository PRs, and `[]` admits only those PRs. Strict-mode `pull_request_target` requires `forks: []`, exact `owner/repo` entries, or deliberately trusted `owner/*` patterns. Omission, `"*"` anywhere in a list, expressions, and other wildcard patterns are rejected before checkout exemptions. Neither `acknowledge-risk` nor trusted/disabled checkout bypasses admission; actor authorization and checkout restrictions remain mandatory. Non-strict mode retains unrestricted target admission for omission or `"*"`. The same-repository default for configured `pull_request` event objects is unchanged.

The supported frontmatter schema in `pkg/parser/schemas/main_workflow_schema.json` accepts string and list patterns for both events, but accepts the `forks: {author-association: [...]}` form from [ADR-69374](69374-fork-pr-admission-by-author-association.md) only for `pull_request`; the shared helper's ability to process that object does not make it supported for `pull_request_target`, and this decision does not extend the schema. To produce valid lock files without silently dropping configured admission rules, the cleanup state machine will leave compiler-only subsections before detecting the next event and track `forks` indentation for nested content, including the supported two-space and four-space event layouts.

### Alternatives Considered

#### Alternative 1: Reject `forks:` under `pull_request_target` at schema/compile time

The compiler could treat `forks` as a `pull_request`-only key and raise an error when it appears under `pull_request_target`. That would also stop the silent drop. It was rejected because the field is documented and useful: authors of `pull_request_target` workflows sometimes want to admit only specific fork owners. An error would take away that control instead of providing it.

#### Alternative 2: Apply the same same-repository default to `pull_request_target`

Using one rule for both events would be simpler: omitting `forks` would block forks everywhere. Strict target compilation instead rejects omission and requires maintainers to state the intended trust boundary explicitly. Non-strict mode retains its asymmetric defaults for compatibility.

#### Alternative 3: Fix only the cleanup leak and keep `pull_request`-only fork filtering

A narrower bug fix would remove the leaked keys and leave admission logic alone. It was rejected because removing `forks` from `pull_request_target` without compiling it into a condition would make the silent drop permanent. The author would get a lock file that looks valid but does not enforce the restriction they configured.

### Consequences

#### Positive
- Recognizing PR events after compiler-only `on.*` fields prevents `forks`/`names` from leaking into native triggers and failing Actions schema validation.
- An explicit `forks:` pattern under `pull_request_target` is now enforced as an event-scoped job condition instead of being ignored.
- Event-scoped guards let workflows that set both PR events use independent fork filters, and existing `if:` and label (`names`) conditions are kept.
- Strict-mode target workflows cannot silently admit arbitrary forks, including through risk acknowledgment or checkout exemptions.

#### Negative
- Existing strict-mode target workflows with omitted or unrestricted fork admission must migrate to explicit bounded admission and recompile. This is documented as a breaking change.
- Non-strict mode retains different defaults for the two PR events.
- Recompiling workflows that already had a `forks:` key under `pull_request_target` will start enforcing it. This could stop runs that previously, and unintentionally, admitted all forks.
- The line-oriented cleanup state machine depends on event-ordering logic and indent tracking, which remain fragile compared with editing the YAML AST.
- The author-association object form remains unavailable for `pull_request_target`, despite the shared helper accepting it internally.

#### Neutral
- The [trigger reference](../src/content/docs/reference/triggers.md) describes the `pull_request_target` pattern-filter semantics explicitly.
- Regression tests in `pkg/workflow/filters_forks_test.go`, `pkg/workflow/frontmatter_on_section_cleanup_test.go`, and `pkg/workflow/pull_request_target_forks_test.go` cover fork conditions, generated Actions schema validation, compiler-only field ordering, indentation, strict bounded admission, and non-strict compatibility.
- Fork admission does not grant permissions or secrets and does not make checking out or executing untrusted fork code safe.
