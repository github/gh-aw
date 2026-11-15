# Agent Performance Analyzer - Latest Run (2026-11-15)

Data basis: `metrics/latest.json` (collection_status complete, window 2026-11-14T02:33Z to 2026-11-15T02:33Z). 367 workflows, 169 executed. 635 runs, 627 executed, 485 successful (77.4%). 358 safe outputs accepted, 0 rejected. 81.4M tokens.
Quality, engagement, and PR merge rate are null in every snapshot. Success rate over executed runs is the only score. Output sampling not performed.
Caveat: total != successful+failed+skipped+action_required on some rows. Verify exact counts before citing.

## Bottom tier (executed >= 3, by success rate)
- auditor 0/21 (0%). Hourly "Collect failure evidence" failing. Logs return HTTP 403, so the time_budget cause is unverified. P1 tracker #77641 closed not_planned 2026-11-15T04:40Z while still failing.
- daily-go-test-parallelizer 0/9 (0%), 5.62M tokens. Seven open "reported incomplete result" issues dated 2026-11-15 (#78058, #78043, #78031, #78019, #77980, #77958, #77944). Parent #77069 closed not_planned 2026-11-14.
- ai-moderator 2/10 (20%), command-gated. timeout-minutes 5 vs avg_duration 12140s. Timeout or queue cause unverified.
- contribution-check 3/6 (50%). add-labels allowlist uses lgtm and needs-work. Label existence not verified (lookup inconclusive).
- aw-failure-investigator 3/4, code-scanning-fixer 3/4. Sample too small.
- pr-specification-reviewer 27/35 (77%), command-gated. Failures trace to the non-agent push_ledger_changes job. Tracker #77383 closed completed 2026-11-14.
- pr-sous-chef 86/106 (81%), command-gated. mattpocock-skills-reviewer 43/52 (83%), 4.74M tokens. avenger 10/11 (91%).

## Top performers (executed >= 3, command-gated caveat)
- test-quality-sentinel 63/63, design-decision-gate 25/25, ponytail-reviewer 48/51, pr-code-quality-reviewer 47/50.

## Named reviewer verdicts
- Matt Pocock Skills Reviewer: REDESIGN. 82.7% success, but 4.74M tokens and output volume swings.
- Design Decision Gate: KEEP. 25/25. Trim token use only.
- Impeccable Skills Reviewer: KEEP. Insufficient data in this window.

## Engine mix (frontmatter-parsed, successful/executed)
copilot 246/294 (83.7%), pi 135/166 (81.3%), codex 74/100 (74.0%), claude 25/36 (69.4%). Five workflows with no frontmatter engine: 1/25 (4.0%).

## Failure attribution
142 failures. auditor 21 + DGTP 9 + ai-moderator 8 = 38 (26.8%).

## Trend
Last 7 days (11-09 to 11-15, partial data): 69.4%. Prior 7 days (11-02 to 11-08): 78.3%. Directional only.

## Corrections to prior record (verified 2026-11-15)
- #77641 is closed not_planned (prior note said open).
- #77383 closed completed 2026-11-14. #77939 closed completed 2026-11-15.
- #77069 closed not_planned 2026-11-14. #63656 and #63657 closed not_planned 2026-09-28.

## Open items
- metrics-collector.md engine block (lines 13-15) still lacks model-provider. No open tracker found (#77023 closed not_planned 2026-11-12). Route to Workflow Health Manager.
- Conversation-job pattern tracker aw_cj01 (#77984) is open. Cite it, do not refile.
- Auditor root cause unverified (logs HTTP 403).
- DGTP prompt-improvement issue from the 2026-11-14 run could not be located by search. Verify before citing.

## Run log (condensed)
- 2026-11-11: ranking from 11-08 snapshot.
- 2026-11-12: full ranking, Pocock/Impeccable/DDG verdicts.
- 2026-11-14: ranking from complete latest.json. Verified stale tracker claims. pr-spec attributed to downstream ledger job (#77383).
- 2026-11-15: full ranking from complete latest.json. Verified closed trackers. Bottom-10 audit with deficiency mapping. Proposed DGTP prompt-redesign, auditor root-cause, and metrics-collector model-provider issues (via safe outputs). Weekly discussion submitted via safe output. Issue numbers not yet known at write time.
