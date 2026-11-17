# Workflow Health — 2026-11-17T04:45Z (Workflow Health Manager)
- Compile: 368/368 executable workflows have `.lock.yml` (lock presence only; compile-validate.txt has just the install line).
- Metrics: `metrics/latest.json` 2026-11-17T02:57Z, window 2026-11-16T02:33Z–2026-11-17T02:33Z, complete. 618 executed, 482 successful, 136 failed.
- Tiers, all 368 measured: Healthy 14, Warning 4, Critical 5, Low-sample 186, Inactive 159 (sum 368).
- Excluding 69 command/mention workflows (299): Healthy 9, Warning 2, Critical 4, Low-sample 158, Inactive 126.
- Critical: ai-moderator 5/17 (#78259 open), avenger 1/2 (#78418 open; last 8 live runs green, consider closing), daily-go-test-parallelizer 0/8 (budget-limited incomplete; many open incomplete-result issues), dependabot-burner 0/3 (cmd; #78416/#78436/#78564), visual-regression-checker 1/2 (no open tracker; #78122/#78185 closed).
- Warning: auditor 12/19 (live: 7 consecutive failures since 11-16T21:28Z at Collect failure evidence; #78066 open; logs 403 difc_forbidden), auto-triage-issues 4/6 (#78429), mattpocock-skills-reviewer 25/33 (cmd), pr-specification-reviewer 27/36 (cmd, avg 4170s).
- Conclusion-job `Handle agent failure` pattern recurred (dependabot-burner §44014831252, visual-regression-checker §44011223351) after tracker #77984 closed not_planned 11-16. Not re-filed; flagged in dashboard.
- Trend: not comparable with 11-16 (partial snapshot). 7/30-day unavailable: 11-10 missing, 11-11/11-12 partial null counts, 11-16 partial.
- Issues: dashboard created (so_IivZUWtgSXKS). No other issues filed (all findings already tracked).
