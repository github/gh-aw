# Workflow Health — 2026-09-22T04:37Z

## P0 RESOLVED: cloud-hypervisor EACCES sandbox regression (chain: #61528 → #61952 → #62310)
PR github/gh-aw#62406 ("Migrate agentic workflows to Docker runtime"), merged
2026-09-21T16:58:35Z, migrated 150 workflows off `cloud-hypervisor` to Docker runtime, resolving
the multi-day EACCES crash. Verified: `grep -rl "cloud-hypervisor" .github/workflows/*.md` now
returns only `daily-fact.md` (down from 150+); zero new EACCES occurrence issues filed after the
merge timestamp (last pre-merge occurrence: #62405 at 16:48Z, 10min before merge); `lint-monster`
succeeded on its first post-merge scheduled run (2026-09-22T02:39Z, run §35680245279, no
EACCES/cloud-hypervisor trace in job log). Posted resolution evidence as a comment on tracker
#62310 (recommended maintainers close it) and on dashboard #62311 — **did not** re-open a new
tracker since the defect is fixed.

## Residual watch item (P2, downgraded from P0)
`daily-fact.md` remains intentionally on `cloud-hypervisor` (per #62406's own description) and its
last run (2026-09-21T14:22:47Z, pre-merge) still failed with the old EACCES signature. Needs one
more scheduled run post-merge to confirm recovery; if it still fails, it needs a dedicated,
narrowly-scoped fix (not an ecosystem-wide P0 re-file).

## failing-workflows.json triage (4 entries, re-verified via gh run list + job logs + PR search)
- **lint-monster**: was 5 consecutive EACCES failures (09-17→09-21); succeeded 2026-09-22T02:39Z
  post-#62406 merge.
- **daily-firewall-report**: was 5 consecutive EACCES failures; next run (2026-09-22T02:29Z) was
  `cancelled` (different signal, not a recurrence of the EACCES crash pattern).
- **daily-go-test-parallelizer**: fully recovered — 6/6 most recent runs successful, unaffected by
  the P0 throughout.
- **cjs**: plain GH Actions workflow, out of `gh aw` scope — 9/10 recent runs successful, 1
  `action_required` (approval gate, not a failure).

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Metrics staleness
`metrics/latest.json` still dated 2026-09-01 — 21 days stale, 10th consecutive affected run.
Metrics Collector was itself an EACCES casualty of the now-resolved P0; expect it to recover on
its next scheduled run post-#62406. Continued cross-checking `failing-workflows.json` entries live
via `gh run list`/job logs/PR search rather than trusting the stale snapshot.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s `update-issue` safe-output still lacks `target: '*'`, so this
schedule-triggered workflow cannot update/close existing issues directly — used `add_comment` on
both #62310 (tracker) and #62311 (dashboard) this run instead of `update_issue`. Recommendation
unchanged: add `update-issue: target: '*'` to enable true issue-refresh/close behavior on
scheduled runs.

## Actions Taken This Run (2026-09-22)
- Verified PR #62406 merge resolves the long-running cloud-hypervisor EACCES P0 (grep evidence +
  issue-creation-timestamp cutoff + post-merge successful lint-monster run).
- Posted resolution comment on P0 tracker #62310 (recommend close).
- Posted resolution/status comment on dashboard #62311 (material delta: P0 resolved).
- Flagged `daily-fact` as a residual single-workflow P2 watch item.
- No new maintenance issues created — all findings resolved or already tracked.

> Last updated: 2026-09-22T04:37Z
