# ADR-78779: Scatter Day, Week, and Month Interval Schedules via Fuzzy Tokens

**Date**: 2026-11-17
**Status**: Draft
**Deciders**: PR maintainers (acceptance pending)

---

### Context

Natural-language interval schedules `every N days`, `every Nw`, and `every Nmo` (including `every 1d`, `every 1w`, and `every 1mo`) were compiled by `pkg/parser/schedule_parser.go` into fixed midnight crons, for example `every 2 days` to `0 0 */2 * *`. The fuzzy scatter stage (`ScatterSchedule` in `pkg/parser/schedule_fuzzy_scatter.go`) never received these schedules, so every workflow using them fired at 00:00 UTC. The PR description reports 63 workflows (62 of them smoke tests) using `every 2 days` firing in the same minute. The compiled cron must stay deterministic across recompilations so lock files do not drift, and the fix should reuse the existing scatter infrastructure.

### Decision

We will compile day, week, and month interval schedules to fuzzy tokens (`FUZZY:DAILY`, `FUZZY:WEEKLY`, `FUZZY:DAILY_INTERVAL/N`, and `FUZZY:MONTHLY_INTERVAL/N`) and resolve them in the scatter layer. Hour and minute come from the existing `weightedDailyTimeSlot(workflowIdentifier)`, and monthly intervals use a hashed day of month in 1-28. The primary driver is avoiding load pile-ups at 00:00 UTC. Scattering per workflow identifier keeps recompilation idempotent. Scattering the weekday for `every 1w` and date for `every 1mo` is intentional: interval expressions distribute calendar slots, while `weekly on sunday` or raw cron preserves calendar constraints.

### Alternatives Considered

#### Alternative 1: Keep fixed midnight crons and document `cron:` for scattering

Leaving the parser unchanged avoids a one-time cron change in existing lock files. It was rejected because the pile-up is the defect this change targets, and it pushes the mitigation onto every workflow author. Authors who need an exact time can still write a raw `cron:` expression, and this decision preserves that escape hatch.

#### Alternative 2: Compile-time random offset stored as a plain cron

Pick a random time at compile time and write it as a fixed cron. It was rejected because the output changes on every recompilation, causing silent schedule drift and noisy lock-file diffs. The hash-based approach is deterministic per workflow identifier, the same reasoning used in ADR-42524.

#### Alternative 3: Scatter only the minute and keep the midnight hour

This is the smallest change, producing `M 0 */N * *`, and it matches how `every Nh` schedules scatter only the minute. It was a close call. It was rejected because every workflow would still fire within the 00:00-00:59 UTC window, leaving most of the pile-up in place. The chosen approach scatters hour and minute together through the weighted daily slot.

### Consequences

#### Positive

- Workflows sharing an interval are spread across the day. The PR description reports that the 63 `*/2` workflows now spread over 18 hours, with at most 8 in any hour and at most 2 in the same minute.
- The change reuses the existing `weightedDailyTimeSlot` and `stableHash` helpers. The new surface is two handlers and two token families.
- Output is deterministic per workflow identifier, so recompilation is idempotent and lock-file diffs are stable.
- `every 1d` and `every 1w` now share the code path used by `daily` and `weekly`.

#### Negative

- Recompiling changes the cron for every existing workflow that uses these forms. This PR recompiles 64 lock files, a one-time schedule shift for affected users.
- `every 1w` changes from a fixed Sunday to a per-workflow weekday, because `handleWeekly` hashes the weekday across 0-6. Users who depend on Sunday runs can use `weekly on sunday`; the changeset and schedule reference document this migration.
- `every 1mo` changes from the 1st of the month to a hashed day in 1-28.
- Users who need an exact time must switch to a raw `cron:` expression.
- `ScatterSchedule` now depends on handler ordering. `handleDailyInterval` must run before `handleDaily` because `FUZZY:DAILY` is a prefix of `FUZZY:DAILY_INTERVAL`. Prefix matching makes this fragile for future token additions.

#### Neutral

- Any tooling that inspects cron values mid-pipeline must understand the new `FUZZY:DAILY_INTERVAL/N` and `FUZZY:MONTHLY_INTERVAL/N` tokens, as it already must for `FUZZY:EVERY_MINUTE` (ADR-42524).
- `every Nh` schedules still scatter only the minute, not the hour. This is out of scope for this decision; the PR description notes that 7 workflows in this repository use that form.
- The `*/N` day-of-month step keeps the semantics of the previous fixed crons. It resets at each month boundary, so it is not an exact N-day period. This PR does not change that behavior.
- New tests assert ranges and distinctness rather than exact crons for every workflow. `compiled_lock_files_test.go` now sets a workflow identifier, matching the CLI.
- The changeset is a `minor` bump.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
