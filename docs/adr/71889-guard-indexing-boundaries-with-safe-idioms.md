# ADR-71889: Guard Indexing Boundaries with Safe Idioms Instead of Analyzer Suppression

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent (PR #71889) [TODO: verify]

---

### Context

The custom lint scan (LintMonster, issue #71532) reported 1,473 direct slice/string indexing findings across 300 files in `pkg/`. Some represent real failure modes. For example, empty color palettes in `pkg/logger`, negative token offsets in `pkg/parser/schedule_parser.go`, out-of-range reference-update indices in `pkg/cli/update_actions_content_refs.go`, and a cron step expansion in `pkg/cli/compile_schedule_calendar.go` that can overflow `int` and produce negative calendar-grid indices. Many other findings depend on invariants such as regex result shapes, companion-slice lengths, or caller contracts. The analyzer cannot prove those invariants. The issue asks for minimal, behavior-preserving fixes, with no suppressions and focused tests when boundary behavior changes.

### Decision

We will fix indexing findings one at a time. Each fix either adds an explicit bounds guard that degrades gracefully, by returning a zero value, an error, a plain fallback, or `continue`/`break`, or uses a standard-library idiom that cannot index out of range. These idioms are `strings.Cut`/`strings.CutPrefix`, `strings.HasPrefix`/`HasSuffix`, `range` iteration, and `append` onto a zero-length, pre-sized slice instead of `make(len)` plus index assignment. We will not change the analyzer or add suppressions. Findings that rely on invariants the analyzer cannot see stay open and visible. Each changed boundary behavior gets a regression test, including empty inputs, invalid offsets, boundary indices, and `math.MaxInt` cron steps.

### Alternatives Considered

#### Alternative 1: Suppress or Teach the Analyzer About Safe Patterns

Add `//nolint`-style suppressions, or extend the custom analyzer so it recognizes invariant-backed accesses such as regex submatch shapes or modulo-by-length. This would clear far more of the 1,448 remaining findings quickly. We rejected it because the issue explicitly forbids suppressions. Analyzer changes would also widen the PR's scope and could hide real defects behind assumed invariants.

#### Alternative 2: Fix All Findings in One Sweep

Rewrite all 1,473 indexing sites across 300 files in a single change. This would close the lint category completely. We rejected it because many sites need a case-by-case review of caller contracts. A bulk rewrite risks silent behavior changes across unrelated subsystems, and the result would be very hard to review.

#### Alternative 3: Let Panics Surface and Recover at Boundaries

Leave the indexing as written and rely on `recover()` or process-level crash reporting. This needs no per-site changes. We rejected it because a panic in the compiler or CLI gives users a poor experience. Some failure modes are also silent rather than panics, such as integer overflow that produces negative indices.

### Consequences

#### Positive
- Real crash and overflow paths, such as empty palettes, negative offsets, and `MaxInt` cron steps, are now handled deterministically and covered by tests.
- `strings.Cut`/`HasPrefix` and append-based construction remove whole classes of off-by-one and out-of-range errors without hand-written checks.
- Valid-input behavior is unchanged, which keeps the change low-risk.

#### Negative
- The lint category stays unresolved. 1,448 findings across 294 files still need follow-up PRs.
- Graceful fallbacks, such as returning the uncolored namespace, falling back to bytes, or skipping invalid updates, can hide upstream bugs that would otherwise fail loudly.
- Some guards check conditions that current callers can never trigger. This adds defensive code with limited runtime value.

#### Neutral
- `pkg/logger/slog_adapter.go` now formats slog attributes directly instead of using an intermediate key/value slice, and the `fmt` import is gone.
- Future indexing fixes should follow the same pattern: a guard or safe idiom, plus a focused boundary test, with no suppression.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
