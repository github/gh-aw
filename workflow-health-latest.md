# Workflow Health Summary - 2026-11-21 (run 44866176115)

Timestamp: 2026-11-21T04:30Z (approx.)
Scored snapshot: metrics/daily/2026-11-17.json (complete, 24h window 2026-11-16T02:33Z to 2026-11-17T02:33Z). latest.json is partial and is not scored. Snapshots 11-18, 11-19, 11-20 are partial, so no 7d/30d trend.
Cohort: 368 snapshot records. Inventory: 370 .md workflows, all 370 have .lock.yml. 5 inventory workflows have no 11-17 record; 3 snapshot records are no longer in inventory. Compile validation unavailable (pre-computed output only says "Successfully installed").

## Tiers (2026-11-17, sum 368)
- Healthy 14, Warning 4, Critical 5, Low-sample 186, Inactive 159 (matches 11-16 dashboard)
- Critical: ai-moderator 5/17 (24h: 4 success, 0 failure, 56 skipped/other), avenger 1/2 (24h: 24/24 success), daily-go-test-parallelizer 0/8 (24h: 8 of 11 completed failed), dependabot-burner 0/3 (1 schedule success 11-20), visual-regression-checker 1/2 (24h: 6 success, 2 failure, both 11-20 17:46-18:30Z)
- Warning: auditor 12/19 (24h: 6 of 23 completed failed, latest 11-21 03:29Z), auto-triage-issues 4/6, mattpocock-skills-reviewer 25/33 (command gating), pr-specification-reviewer 27/36 (command gating)

## Actions this run
- New P1 tracker: daily-go-test-parallelizer conclusion-job failure (untracked before this run)
- Comment on P1 Auditor tracker #79418 with 24h status
- Dashboard 2026-11-21 filed (material delta: new untracked Critical workflow)

## Corrections to prior notes
- dependabot-burner: prior note "failed 6 of 6 (last 11-18)" is stale. Latest run 11-20 06:24Z succeeded.
- ai-moderator: 0 failures in 24h; Critical tier reflects 11-17 history.
- "Handle agent failure" conclusion-job failures also seen in daily-go-test-parallelizer (8 of 11, 24h) and visual-regression-checker; root cause unverified (run logs HTTP 403 DIFC).
- Expired trackers (#78921-#78923, #78066) closed by create-issue expires 1d, not not_planned. Maintenance issue #79421 tracks this.

## Run outcome (2026-11-21T04:30Z)
- Filed P1 tracker for daily-go-test-parallelizer conclusion-job failures (safe-output so_v23MzeRqTE-F).
- add_comment on Auditor P1 #79418 was rejected: schedule-triggered run has no add-comment target. The 24h status is recorded in the dashboard instead.
- Dashboard issue for 2026-11-21 filed as the terminal safe-output.
- push_repo_memory is not in the tool list; memory files persist via the workflow's automatic commit.
