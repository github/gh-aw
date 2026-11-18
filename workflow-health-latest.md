# Workflow Health — 2026-11-18T04:30Z (Workflow Health Manager)
- Inventory: 366 root `.md` workflows, 366 `.lock.yml`, 0 gaps (`shared/` excluded). `compile-validate.txt` contains only the install line, so compile warnings are unavailable.
- Metrics: `metrics/latest.json` 2026-11-18T02:59Z is PARTIAL (2.25h window, 341/366 run coverage unavailable). Not scored.
- Scored snapshot: `daily/2026-11-17.json` complete, window 2026-11-16T02:33Z–2026-11-17T02:33Z. 618 executed, 482 successful (78.0%), 136 failed.
- Tiers, 368 metrics entries: Healthy 14, Warning 4, Critical 5, Low-sample 186, Inactive 159 (sum 368). Excluding 69 command/mention workflows (299): Healthy 9, Warning 2, Critical 4, Low-sample 158, Inactive 126.
- Metrics cohort includes `engine-conformance-atomic` and `smoke-atomic`, which have no source `.md`. Treat as cohort mismatch, not inventory.
- Critical: ai-moderator 5/17, avenger 1/2, daily-go-test-parallelizer 0/8, dependabot-burner 0/3, visual-regression-checker 1/2.
- Warning: auditor 12/19, auto-triage-issues 4/6, mattpocock-skills-reviewer 25/33 (cmd), pr-specification-reviewer 27/36 (cmd).
- Live (gh run list, 2026-11-18): auditor 21 failures in last 24 runs; 2 consecutive failures now (03:29Z, 02:30Z), after successes 01:27Z and 00:45Z. Failing step: `Collect failure evidence`. Logs blocked (HTTP 403 DIFC), root cause unverified.
- Live: dependabot-burner 5 failures in ~24h, all at `conclusion` (`Handle agent failure`). visual-regression-checker 7 failures in ~24h, same pattern. avenger 6 successes in a row (recovered).
- Closed not_planned while still failing: #78066 (Auditor, 11-17T14:40Z), #78416/#78436/#78564 (Dependabot Burner), #78259 (AI Moderator), #78122/#78185 (Visual Regression Checker), #78429 (Auto-Triage).
- Issues filed this run: P1 Auditor tracker and dashboard (see safe-output records).

## Issues filed this run (2026-11-18)

- P1 Auditor tracker (`workflow-health`, `priority-p1`, `type-failure` labels omitted on this call; replaces closed #78066).
- P1 conclusion-job tracker: dependabot-burner and visual-regression-checker at `Handle agent failure` (labels set; replaces closed #77984).
- Dashboard: "Workflow Health Dashboard - 2026-11-18" (label `workflow-health`).
- Issue numbers are assigned after the run and are not recorded here.
- Memory validated via push_repo_memory: 4 files, 40 KB.
