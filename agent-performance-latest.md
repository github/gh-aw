# Agent Performance Analyzer - Latest Run (2026-11-21)

## 2026-11-21 run (noop)
- Data basis: unchanged. Last complete scored snapshot is `metrics/daily/2026-11-17.json` (368 workflows, 618 executed, 78.0% success, 195 primary outputs, 88.35M tokens). 11-18, 11-19, 11-20 and `latest.json` (window 11-19 to 11-20) are partial and not scored.
- New verified finding: `pr-sous-chef` (lock file `pr-sous-chef.lock.yml`) had 57 failures out of 123 live runs in the 2026-11-19T02:57Z to 2026-11-20T02:37Z window. Since 2026-11-20T17:47Z, 99 of 100 live runs succeeded. The partial 11-20 snapshot (43 failures) undercounts the live data. This is an operational burst that has recovered, so it is routed to Workflow Health, not scored as a quality decline.
- Fix trackers closed `not_planned` exactly 24h after creation: #79421 (maintenance for `expires: 1d`, created 11-20T04:40Z, closed 11-21T04:40Z) and #79418 (Auditor P1, same timing). The expiry problem also blocks its own fix tracker. Recommendation carried into the 11-22 weekly report, not refiled.
- Decision: noop. Weekly discussion is due 2026-11-22. No new issue, since the expiry recommendation is already on record and a new filing would duplicate it.

## 2026-11-20 run (noop)
- Data basis: last complete scored snapshot is `metrics/daily/2026-11-17.json` (368 workflows, 618 executed, 78.0% success, 195 primary outputs, 88.35M tokens; re-verified). 11-18, 11-19, 11-20 snapshots and `latest.json` are partial and not scored.
- Tracker states re-verified via `github issue_read` (state fields only): #78066 CLOSED not_planned, closed_at 2026-11-17T14:40:35Z (created 11-15T12:53Z; ~2d, matches `expires: 2d`). Correction: earlier notes give 12:53Z as the close time; 12:53Z is the expiry time. #77984 CLOSED not_planned 11-16T04:48Z. #78921, #78922, #78923 CLOSED not_planned 11-19T04:40Z (created 11-18T04:39Z, ~24h, matches `expires: 1d`). #78259, #78416, #78418, #78429 CLOSED not_planned 11-17 (created 11-16). No open tracker is verifiable: open-issue list fully integrity-filtered (30 of 30 hidden).
- Likely self-filed: #78066 carries the `cookie` label and a 2d expiry, and the 11-15 run logged 2 issues. Inferred, not verified via author. If so, this workflow's own `expires: 2d` also auto-closes its trackers while failures persist.
- Inconsistency confirmed for mattpocock-skills-reviewer, comments_added by complete day: 11-13 0, 11-14 0, 11-15 39, 11-17 0 (11-16 partial 49). Exec 19/32/52/33.
- Decision: noop. The expiry-closure recommendation (2026-09-21, 2026-11-14, 2026-11-17) already covers this. Filing a new issue would duplicate it. Next weekly discussion due 2026-11-22.

Data basis: `metrics/latest.json` (collection_status complete; window 2026-11-16T02:33Z to 2026-11-17T02:33Z). Quality and engagement fields are null, so scores use executed-run success rate only. No output sampling was done.
Totals (11-17): 618 executed, 78.0% successful, 88.3M tokens, 195 safe outputs.
Daily overall rate: 11-13 55.3%, 11-14 72.4%, 11-15 77.4%, 11-16 77.4% (partial), 11-17 78.0%.

## Ranking (complete 11-17, executed >= 3; cmd = command-triggered, rate on executed runs)
Top: issue-monster 70/70 (100%); ponytail-reviewer 35/37 (94.6%, cmd, 8.46M tokens); pr-sous-chef 77/83 (92.8%, cmd, 18 issues and 21 comments); test-quality-sentinel 38/41 (92.7%); pr-code-quality-reviewer 30/33 (90.9%); design-decision-gate 13/15 (86.7%, cmd, 0.16M tokens).
Middle: contribution-check 5/6 (83.3%, 5 issues created); auto-triage-issues 4/6 (66.7%, 1.28M tokens); mattpocock-skills-reviewer 25/33 (75.8%, cmd, 2.72M tokens); pr-specification-reviewer 27/36 (75.0%, cmd, avg 4170s).
Bottom: auditor 12/19 (63.2%); ai-moderator 5/17 (29.4%, avg 10603s); daily-go-test-parallelizer 0/8 (0%, 7.63M tokens); dependabot-burner 0/3 (cmd, 2.40M tokens).
Small samples (<3 executed): impeccable-skills-reviewer 2/2; visual-regression-checker 1/2; ab-testing-advisor 0/1 (7.41M tokens); audit-workflows 0/1 (7.32M tokens).

## Trend notes
- auditor: 0/21 (11-15), 2/21 (11-14), 12/19 (11-17). Partial recovery. Workflow Health reports 7 consecutive live failures at "Collect failure evidence" since 11-16T21:28Z.
- ai-moderator: 9/16 (11-13), 12/18 (11-14), 2/10 (11-15), 5/17 (11-17). Not stable.
- daily-go-test-parallelizer: 0/8 (11-17). Failures are agent-declared budget-limited incompletes (Workflow Health). Parent #77069 closed not_planned.
- mattpocock-skills-reviewer comments: 0 (11-13), 0 (11-14), 39 (11-15), 49 (11-16 partial), 0 (11-17). Output volume is inconsistent.

## Bottom-10 prompt audit (grep markers: self-check, rubric, success-criteria, examples)
1. daily-go-test-parallelizer (133 lines; 2 self-check, 0 rubric, 0 criteria). The earlier "contradictory tool guidance" finding is STALE. The prompt now permits `go test` validation and forbids pre-screening. Remaining deficiency: budget and turn limits produce incompletes, and the prompt sets no success criteria.
2. auditor (851 lines; 1 self-check, 0 rubric, 1 criteria). Logs return HTTP 403 (difc_forbidden), so the prompt deficiency is NOT established. Tracker #78066 is OPEN (verified 11-17).
3. ai-moderator (315 lines; 1 self-check, 1 rubric, 0 criteria). `timeout-minutes: 5` against a 10,603s average may be queue time in the metric. Unverified. Route to Workflow Health; not a prompt finding.
4. dependabot-burner (582 lines; 0 self-check, 0 rubric, 0 criteria, cmd). 0/3. Route to Workflow Health.
5. mattpocock-skills-reviewer (326 lines; 0 self-check, 0 rubric, 1 criteria, 4 examples, cmd). Missing self-assessment loop. Verdict: REDESIGN.
6. pr-specification-reviewer (326 lines; 0 self-check, 0 rubric, 1 example, cmd). Failures trace to the non-agent push_ledger_changes job (#77383 closed completed 11-14). Not a prompt issue.
7. pr-sous-chef (393 lines; 0 self-check, 0 criteria, cmd). Missing self-assessment loop. Watch item; no baseline for over-creation.
8. contribution-check (339 lines; 0 self-check, 0 criteria, 5 examples). 1 failed run of 6. Earlier "bad labels" alert (lgtm/needs-work) is STALE: get_label returned both names. Failure cause otherwise unverified. Sample too small.
9. auto-triage-issues (392 lines; 0 self-check, 0 criteria, 2 examples). 2 failed runs of 6. Sample too small.
10. visual-regression-checker (116 lines; 0 self-check, 0 rubric, 0 criteria). 1/2 executed. Missing success criteria, self-assessment loop, and rubric. Sample too small to act on.

## Named reviewer verdicts
- Matt Pocock Skills Reviewer: REDESIGN (unchanged). 75.8% success; comment-volume swings; no self-check or rubric.
- Design Decision Gate: KEEP. 13/15 (86.7%), 0.16M tokens (~10K/run). Earlier "trim tokens" note is not supported; drop it.
- Impeccable Skills Reviewer: KEEP. 2/2 (100%). Insufficient data.

## Pattern labels (count-only rule; a quiet agent can be correct)
- under-creation (>= 3 executed, 0 primary safe outputs in the 11-17 window): auditor (12 successful, 0 outputs), daily-go-test-parallelizer (0 successful, 0 outputs). Confirm intent before acting.
- under-creation candidates (0 outputs of every type in the 11-17 window; small samples): test-quality-sentinel (38/41; zero in 5 of 6 complete snapshots since 11-03), code-scanning-fixer (4/5), aw-failure-investigator (4/4). A quiet agent can be correct. Confirm intent before acting.
- ai-moderator removed from under-creation: 11-17 by-type shows 4 add_labels (0 primary outputs; labels are not counted as primary).
- inconsistency: mattpocock-skills-reviewer (see trend notes).
- over-creation, repetition, scope-creep: none flagged from profile data.

## Resource outliers (11-17 tokens)
ponytail-reviewer 8.46M (37 runs, ~229K/run); daily-go-test-parallelizer 7.63M (0 successful); ab-testing-advisor 7.41M (1 run, failed); audit-workflows 7.32M (1 run, failed); mattpocock-skills-reviewer 2.72M (33 runs); dependabot-burner 2.40M (0/3).

## Verified this run (2026-11-17, via github issue_read)
- #78066 (Auditor) OPEN, created 2026-11-15T12:53Z.
- #78259 (AI Moderator) CLOSED not_planned: created 11-16T04:51Z, closed 11-17T04:58Z. Workflow Health's 11-17 note still lists it as open; that note is stale.
- #78416 (Dependabot Burner) CLOSED not_planned: created 11-16T16:14Z, closed 11-17T04:40Z.
- #78418 (Avenger) CLOSED not_planned: created 11-16T16:29Z, closed 11-17T04:40Z. Workflow Health's note still lists it as open; stale.
- #78429 (Auto-Triage Issues) CLOSED not_planned: created 11-16T18:25Z, closed 11-17T06:44Z.
- #77062 CLOSED not_planned: created 11-12T04:37Z, closed 11-13T04:40Z (~24h).
- #77641 CLOSED not_planned: created 11-14T04:38Z, closed 11-15T04:40Z (~24h).
- #77069 CLOSED not_planned (DGTP parent, 11-14). #77383 CLOSED completed (11-14). #77984 CLOSED not_planned (11-16; earlier notes calling it open are stale; pattern not re-verified).
- Pattern: WH-filed trackers close not_planned 12-24h after creation while their workflows still fail. Matches `expires: 1d` (group: true) in workflow-health-manager.md, lines 27-29. The 2026-09-21 and 2026-11-14 recommendations still apply.
- Search results are integrity-filtered (5 to 10 items removed per query). Duplicate checks are unreliable.
- metrics-collector.md still has no model-provider under engine (codex, model copilot/gpt-6-luna). Whether this is required is unverified. Its 0 runs are likely window artifacts.

## Decision this run
Weekly report already published 11-15; next weekly due 11-22. No new discussion or issue: the expiry-closure pattern is already recommended to maintainers (2026-09-21, 2026-11-14), and a new filing would duplicate. Terminal output: noop.

## Run log (condensed)
- 2026-11-11: ranking from 11-08 snapshot.
- 2026-11-12: full ranking, Pocock/Impeccable/DDG verdicts.
- 2026-11-14: ranking from complete latest.json. pr-spec attributed to downstream ledger job.
- 2026-11-15: full ranking, bottom-10 audit, weekly discussion, 2 issues.
- 2026-11-16: ranking from complete 11-15 snapshot. Verified trackers. Corrected aw_cj01, DDG tokens, and labels. noop.
- 2026-11-17: ranking from complete 11-17 snapshot. Verified trackers via github issue_read. Corrected DGTP prompt note. Recorded expiry-closure pattern. noop.
- 2026-11-18: re-verified 11-17 complete snapshot (618 executed, 482 successful, 88.35M tokens, 195 primary outputs; all match). 11-18 snapshot partial (2.25h window), not scored. Corrected ai-moderator under-creation; added zero-output candidates. noop.
- 2026-11-20: 11-17 snapshot re-verified; 9 tracker states re-verified; #78066 closed_at corrected; mattpocock inconsistency confirmed over 5 complete/partial days. noop.

## 2026-11-22 run (weekly report filed)
- Data basis: last complete snapshot `metrics/daily/2026-11-17.json` (368 workflows, 618 executed, 78.0% success). 11-18 to 11-22 and `latest.json` are partial and unscored.
- Report: weekly performance discussion filed (so_ubSVMTvIQ-q8). Includes rankings, bottom-10 prompt audit, named verdicts (Matt Pocock REDESIGN, Impeccable KEEP, Design Decision Gate KEEP), pattern labels, and maintainer decisions.
- Verified: #79421 and #79418 closed not_planned at 24h (expires:1d pattern). #79686 closed completed while failures continue.
- Open: expires:1d maintainer decision; ai-moderator timeout (5 min vs 10,603s average) unverified as queue time; DGTP 25-credit and 60-turn budget decision.
