# Workflow Health — 2026-09-21T04:44Z

## P0 (re-filed): cloud-hypervisor EACCES sandbox regression — predecessor #61952 expired unfixed
Predecessor tracker #61952 (filed 2026-09-19T04:42Z) was auto-closed 2026-09-20T04:44Z by its
`expires: 1d` safe-output setting (`NOT_PLANNED` / "automatically closed because it expired") —
**no fix PR ever landed**. This is the 3rd tracker in the chain (#61528 → #61952 → new). 30 open
`[aw] <workflow> failed` issues match this signature as of this run, spanning a wider set of
workflows than the predecessor's 44-occurrence snapshot two days ago (new: GPL Dependency Cleaner
#62298, Metrics Collector #62290, Code Simplifier #62297, Auto-Triage Issues #62213, several Smoke
tests). Confirmed via direct job-log inspection of LintMonster run §35555065706:
`spawn /run/awf-cloud-hypervisor/trusted-artifacts/run-nF74bu/cloud-hypervisor EACCES`. No merged
PR found addressing exec-bit/mount permissions for the trusted-artifact binary (checked PR search
for `cloud-hypervisor`, `EACCES`, `trusted-artifact`, `chmod` — no matches).
**Action:** Filed a new consolidated P0 tracker this run (superseding expired #61952) — DO NOT
re-file per-workflow duplicates. Recommended `priority-p0` labels be exempted from `expires: 1d`
since the fix cadence has now exceeded 24h twice.

## failing-workflows.json triage (4 entries, re-verified via gh run list + job logs)
- **lint-monster**: 5 consecutive failures (09-17→09-21), same EACCES crash.
- **daily-firewall-report**: 5 consecutive failures (09-17→09-21), same EACCES crash (signature
  confirmed via prior-day logs; today's run log fetch hit a transient 404 but failure pattern is
  consistent with lint-monster and the tracked P0).
- **daily-go-test-parallelizer**: fully recovered — 6/6 most recent runs successful.
- **cjs**: plain GH Actions workflow, out of `gh aw` scope — 9/10 recent runs successful, 1
  `action_required` (approval gate, not a failure).

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Metrics staleness
`metrics/latest.json` still dated 2026-09-01 — 20 days stale, 9th consecutive affected run. Root
cause unchanged: Metrics Collector is itself an EACCES casualty of the P0 above. Continued
cross-checking `failing-workflows.json` entries live via `gh run list`/job logs rather than
trusting the stale snapshot.

## Tooling limitation (carried over, unresolved)
`workflow-health-manager.md`'s `update-issue` safe-output still lacks `target: '*'`, so this
schedule-triggered workflow cannot update an existing dashboard/tracker issue directly — used
`create_issue` for both the new P0 tracker and the new dashboard issue this run instead of
`update_issue`. Recommendation unchanged: add `update-issue: target: '*'` to enable true
issue-refresh behavior on scheduled runs.

## Actions Taken This Run (2026-09-21)
- Created new P0 tracker issue: "cloud-hypervisor EACCES sandbox failure — 30 open occurrences,
  predecessor #61952 expired unfixed" (labels: cookie, workflow-health, priority-p0, type-failure).
- Created new dashboard issue: "Workflow Health Dashboard - 2026-09-21".
- No other issues created/updated — all other findings already tracked or explained by expected
  gating behavior (cjs `action_required` = approval gate).

> Last updated: 2026-09-21T04:44Z
