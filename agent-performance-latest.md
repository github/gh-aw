# Agent Performance Analyzer - Latest Run (2026-11-16)

Data basis: `metrics/daily/2026-11-15.json` (collection_status complete). The 2026-11-16 `latest.json` is partial (window 11-15T02:35Z to 11-16T02:35Z; safe-output outcomes all pending, quality and PR merge rate null). Scores below use executed-run success rate only. Output sampling not performed, so no quality scores.
Totals (complete 11-15): 627 executed, 77.4% successful, 81.4M tokens. Daily overall rate: 11-13 55.3%, 11-14 72.4%, 11-15 77.4%. Older complete days: 11-03 73.9%, 11-08 86.9%.

## Ranking (complete 11-15, executed >= 3)
Top: test-quality-sentinel 63/63 (100%, 1.33M tokens), design-decision-gate 25/25 (100%, 0.28M), issue-monster 11/11, auto-triage-issues 3/3 (2.8M tokens, 3 runs), pr-triage-agent 3/3, ponytail-reviewer 48/51 (94%, 11.6M tokens).
Middle: avenger 10/11 (91%), mattpocock-skills-reviewer 43/52 (83%, 4.74M tokens), pr-sous-chef 86/106 (81%, 1.40M tokens), pr-specification-reviewer 27/35 (77%, avg 3803s).
Bottom: aw-failure-investigator 3/4, code-scanning-fixer 3/4, contribution-check 3/6, ai-moderator 2/10, auditor 0/21, daily-go-test-parallelizer 0/9.
Caveat: pr-sous-chef and pr-specification-reviewer are command-triggered per frontmatter. Their executed counts may include gated runs. Not verified per run; read as a caveat.

## Bottom-10 prompt audit (grep counts; self-check, rubric, success-criteria, example markers)
1. daily-go-test-parallelizer 0/9. Contradictory tool guidance. Validation step requires go test, but policy forbids env enumeration. Agent-declared reason: "required Go validation cannot run without prohibited process-environment enumeration" (unverified). 5.62M tokens for 9 runs (~625K/run). Seven open incomplete-result issues (#78058 etc.). Parent #77069 closed not_planned 2026-11-14 (verified).
2. auditor 0/21. 814-line prompt, 1 self-check marker, 0 rubric, 0 examples. Failing step "Collect failure evidence". Logs HTTP 403, so prompt deficiency NOT established. Tracker #78066 is OPEN (verified). Do not redesign prompt until root cause is confirmed.
3. ai-moderator 2/10. timeout-minutes 5 vs avg_duration 12,140s in snapshot. Conflict may be queue time in metric. Unverified. Not a prompt finding. Route to Workflow Health.
4. contribution-check 3/6. Prior "bad labels" alert (lgtm/needs-work) looks stale: get_label returned both names. Failure cause otherwise unverified.
5. visual-regression-checker 2/5. 116 lines, 0 self-check, 0 rubric, 0 success-criteria markers. Missing success criteria, missing self-assessment loop, no rubric.
6. aw-failure-investigator 3/4, code-scanning-fixer 3/4. Sample < 5. Insufficient data.
7. pr-specification-reviewer 27/35. Failures trace to non-agent push_ledger_changes job (tracker #77383 closed completed 11-14). Not a prompt issue.
8. pr-sous-chef 86/106. 393 lines, 0 self-check, 0 success-criteria markers, 1 TODO/legacy marker to review. Missing self-assessment loop. 1.40M tokens.
9. mattpocock-skills-reviewer 43/52. 326 lines, 0 self-check, 0 rubric, 1 success-criteria marker, 4 example refs. Missing self-assessment loop. Output swings: 0 comments 11-13/11-14, 39 on 11-15, 49 on 11-16 (partial). Verdict: REDESIGN.
10. auto-triage-issues 2/3. Sample too small. 2.8M tokens over 3 runs (~940K/run).

## Named reviewer verdicts
- Matt Pocock Skills Reviewer: REDESIGN. 82.7% success; token and comment-volume swings; no self-check or rubric in prompt.
- Design Decision Gate: KEEP. 25/25; 284K tokens (~11K/run). Prior "trim tokens" note is not supported by 11-15 data; drop it.
- Impeccable Skills Reviewer: KEEP. 1/1 on 11-15, 3/3 on 11-16 partial. Insufficient data.

## Pattern labels (count-only rule; a quiet agent can be correct)
- under-creation (>= 3 executed, 0 safe outputs in 11-15 window): auditor (0/21), daily-go-test-parallelizer (0/9), ai-moderator (0 outputs on 2/10 successful). Also aw-failure-investigator and code-scanning-fixer (0 outputs on 3/4 successful; sample too small). test-quality-sentinel (63/63 successful, 0 outputs) is excluded: it may be silent by design. Confirm intent before flagging.
- inconsistency: mattpocock-skills-reviewer (0 -> 39 -> 49 comments).
- over-creation, repetition, scope-creep: none detected from profile data.

## Resource outliers (11-15 tokens)
spec-enforcer 11.95M (1 run); ponytail-reviewer 11.56M (51 runs, ~227K/run); daily-go-test-parallelizer 5.62M; mattpocock-skills-reviewer 4.74M; daily-doc-updater 4.69M; auto-triage-issues 2.8M.

## Verified this run (2026-11-16)
- #78066 (auditor) OPEN.
- #77984 (aw_cj01 conversation-job pattern) CLOSED not_planned 2026-11-16T04:48Z. Earlier notes calling it open are stale. Pattern not re-verified.
- #77641 CLOSED not_planned 2026-11-15T04:40Z. #77069 CLOSED not_planned 2026-11-14T06:40Z.
- metrics-collector.md still has no model-provider under engine (codex, model copilot/gpt-6-luna). Whether required is unverified. Its 0 runs in the window are likely window artifacts (the snapshot is written after the window closes).
- Search and list reads are integrity-filtered (e.g. 40 items removed). Duplicate-issue checks for prior analyzer issues are unreliable.

## Decision this run
No new discussion or issues. The 2026-11-15 run already published the weekly report and filed analyzer issues (metrics: 1 discussion, 2 issues in window). Filing again would duplicate. Terminal output: noop.

## Run log (condensed)
- 2026-11-11: ranking from 11-08 snapshot.
- 2026-11-12: full ranking, Pocock/Impeccable/DDG verdicts.
- 2026-11-14: ranking from complete latest.json. Pr-spec attributed to downstream ledger job.
- 2026-11-15: full ranking, bottom-10 audit, weekly discussion, 2 issues (via safe outputs).
- 2026-11-16: ranking from complete 11-15 snapshot. Verified trackers. Corrected stale notes (aw_cj01, DDG tokens, labels). noop.
