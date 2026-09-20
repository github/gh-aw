# Workflow Health — 2026-09-20T04:35Z

## P0 (still active, no fix yet): cloud-hypervisor EACCES sandbox regression
Tracker #61952 (filed 2026-09-19T04:42Z) remains open ~24h later with **no fix PR**. 33 new
`[aw] <workflow> failed` occurrence issues opened between 2026-09-19T14:54Z and 2026-09-20T03:50Z
(~13h window), including repeats of previously-cited workflows (LintMonster #62104, Metrics
Collector #62103, Code Simplifier #62109, jsweep #62111). An automated "Issue Monster selected
this for Copilot" assignment comment was posted on #61952 at 2026-09-19T08:48Z but produced no
follow-up PR — checked via `gh api .../issues/61952/timeline` (only cross-ref was an unrelated
closed issue #61908) and PR search (`EACCES is:pr`, `cloud-hypervisor is:pr`) — no open PR
references or fixes this.
**Action:** Posted status-update comment on #61952 quantifying continued occurrence rate and
confirming no fix exists. Did NOT re-file a new tracker (existing #61952 still valid/open, unlike
predecessor #61528 which had auto-expired).

## Tooling limitation confirmed: `update_issue` blocked on scheduled runs
Attempted `update_issue` on the dashboard issue #61953 this run — failed with: "update_issue
requires an issue context but the workflow is running on a 'schedule' event... requires
`update-issue: target: '*'` plus explicit `issue_number`." Workaround used: posted the dashboard
update as an `add_comment` on #61953 instead. **Recommendation for repo maintainers:** if
scheduled-run dashboard updates via `update_issue` are desired, the workflow's safe-outputs config
needs `update-issue: target: '*'` — otherwise all meta-orchestrators must use `add_comment` as a
workaround (as done here and, per shared-alerts.md, by other orchestrators previously).

## failing-workflows.json triage (all 4 entries) — reconfirmed via gh run list
- **lint-monster**: 4 consecutive failures (09-17→09-20), all same EACCES hypervisor crash.
- **daily-firewall-report**: 4 consecutive failures (09-17→09-20), same EACCES crash.
- **daily-go-test-parallelizer**: recovered — 5/5 most recent runs successful.
- **cjs**: plain GH Actions workflow, out of `gh aw` scope — 9/10 recent runs successful.

## Compilation Status
299/299 workflows have lock files (100%), compile-validate clean.

## Metrics staleness
`metrics/latest.json` still dated 2026-09-01 — 19 days stale, 8th consecutive affected run.
Root cause unchanged: Metrics Collector itself is an EACCES casualty of the P0 above.

## Actions Taken This Run (2026-09-20)
- Posted status-update comment on P0 tracker #61952 (no new tracker filed — existing one valid).
- Posted dashboard update as a comment on #61953 (update_issue blocked on schedule trigger, see
  tooling limitation note above).
- No new issues created this run.

> Last updated: 2026-09-20T04:35Z
