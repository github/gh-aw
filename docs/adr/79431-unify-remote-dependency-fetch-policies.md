# ADR-79431: Declare Remote Dependency Fetch Policies in One Ordered Table

**Date**: 2026-11-20
**Status**: Draft
**Deciders**: [TODO: verify] PR #79431 author (gh-aw-bot) and PR actor pelikhan; `pkg/cli` maintainers to confirm

---

### Context

`fetchAllRemoteDependenciesWithOptions` in `pkg/cli/includes.go` fetches six kinds of remote dependency for `gh aw add` and trial: `@include` files, frontmatter imports, runtime imports, dispatch workflows, call-workflow workers, and resources. Each had a hand-written `if err := ...` block, and the failure policy differed between them. `strict` mode affected only includes, frontmatter imports, and runtime imports, while dispatch, call-workflow, and resource failures always returned an error and skipped the steps after them. Because these rules were spread across six call sites, they were hard to review and easy to break when editing, and the existing error prefixes and warning order had to be kept exactly as users see them.

### Decision

We will replace the six hand-written blocks with an ordered table of `remoteDependencyStep` values. Each step declares its label, its run function, and its fatality and warning policy, and a single runner, `runRemoteDependencySteps`, executes the table in order and stops at the first fatal failure. The primary driver is reviewability and testability of per-dependency failure policy; the change is intended to keep existing behavior, and it does not decide which failures should respect `strict`.

### Alternatives Considered

#### Alternative 1: Keep the hand-written blocks and add characterization tests only

Leave the six `if err := ...` blocks unchanged and add tests that pin each step's strict and non-strict outcome. This was the smallest diff, with no new types or closures, and the tests would be needed under any option. It was a close call, but it was rejected because each policy would still live in its own call site, so a new dependency would mean copying one of the six patterns and getting its fatality and warning rules right by hand. The chosen option includes the tests anyway.

#### Alternative 2: Apply one `strict` rule to all six fetchers in the same change

Either make dispatch, call-workflow, and resource failures respect `strict`, or make all six best-effort. This would give one uniform policy and remove the per-step special cases. It was rejected for this change because it alters user-visible behavior: today a failing dispatch fetch in non-strict mode stops the later fetchers, and changing that changes which dependencies are installed. That policy question is tracked separately in #79392 so it can be reviewed on its own.

#### Alternative 3: Per-dependency types behind a shared interface

Define a `dependencyFetcher` interface with one type per fetcher, each exposing `Fetch` and `Policy` methods. This would make each fetcher self-describing, which is a common Go pattern. It was rejected because the six fetchers have different signatures (the includes fetcher takes an options struct, the frontmatter fetcher takes `strict`, and the others take neither), so an interface would still need six adapter types. Closures bind those arguments with less code.

### Consequences

#### Positive
- The strict and non-strict outcome of each dependency is declared in one table, listed in the order the steps run.
- `TestRunRemoteDependencyStepsPolicies` covers all six steps across strict and non-strict and verbose and non-verbose modes (24 cases), checking execution order, error wrapping, and warning counts.
- Adding a dependency means adding a table entry and a fetcher closure, rather than copying another error block.

#### Negative
- Five boolean policy fields (`fatalWhenStrict`, `fatalAlways`, `warnWhenVerbose`, `warnAlways`, `warnBeforeFatal`) interact, and their combinations are not self-describing; a reader has to check `runRemoteDependencySteps` to learn what a combination does. A named policy type would be clearer and is a possible follow-up.
- The runtime-import step needs a custom `warn` closure because its non-strict warning ignores `verbose`, so the table is not fully uniform.
- The fatal error prefix is generated from `label`, but each step's warning text is a separate string, so the two can drift apart over time.
- `includes.go` grows by about 65 net lines (130 added, 65 removed), and the closures capture `ctx`, `content`, `spec`, and related arguments.

#### Neutral
- Behavior is intended to be unchanged: the same messages are printed in the same order, and non-strict mode still stops at the always-fatal steps. This was checked by comparing the old and new code paths; the new tests pin the same policy but were written against the new code, and no before-and-after CLI run was performed.
- Whether dispatch, call-workflow, and resource failures should respect `strict` is deferred to a separate change, tracked in #79392.
- `fetchAllRemoteDependencies` remains the ordered integration point described in [ADR-48282](48282-extend-safe-outputs-dependency-resolution-for-call-workflow.md), so call-workflow ordering is unchanged.
- Callers of `fetchAllRemoteDependencies` and `fetchAllRemoteDependenciesStrict` were not reviewed. [TODO: verify] whether any caller relies on the non-strict early return.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
