# Workflow Health Summary - 2026-11-20 (run 44674692240)

Timestamp: 2026-11-20T04:30Z (approx.)
Scored snapshot: metrics/daily/2026-11-17.json (complete). latest.json is partial (24h, 190 of 369 unavailable) and is not scored.
Cohort: 368 measured. Inventory: 369 executable workflows, all with a .lock.yml. Compile validation unavailable.

## Tiers (2026-11-17)
- Healthy 14, Warning 4, Critical 5, Low-sample 186, Inactive 159 (sum 368)
- Critical: ai-moderator 5/17, avenger 1/2 (recovered), daily-go-test-parallelizer 0/8, dependabot-burner 0/3 (command), visual-regression-checker 1/2
- Warning: auditor 12/19, auto-triage-issues 4/6, mattpocock-skills-reviewer 25/33 (command gating), pr-specification-reviewer 27/36 (command gating)
- 7d/30d trend: unavailable (incomplete daily snapshots 11-16, 11-18, 11-19, 11-20)

## P1 issues created this run
- Auditor failing 11 of 12 hourly runs since 2026-11-19 17:27Z. Root cause unverified (logs HTTP 403 DIFC). Tracker filed.
- Handle agent failure (conclusion job) in dependabot-burner (6/6 failed, last 11-18) and visual-regression-checker (5/6 failed, success 11-19 18:12Z). Tracker filed.
- Maintenance: workflow-health-manager create-issue sets expires 1d, so trackers auto-close. Issue filed.
- Dashboard 2026-11-20 filed.

## Corrections to prior notes
- #78921, #78922, #78923 closed by expiry (2026-11-19T04:39Z, run 44459910663), not not_planned.
- #78066 closed by expiry 2026-11-17 (run 44105641215), not not_planned.
