# Workflow Health Summary - 2026-11-22 (run 44985194287)

Timestamp: 2026-11-22T04:38Z
Scored snapshot: metrics/daily/2026-11-17.json (complete, 24h window 2026-11-16T02:33Z to 2026-11-17T02:33Z). latest.json and daily/2026-11-22.json are partial (~3.7h window) and not scored. 7d/30d trend unavailable (11-18, 11-19, 11-20, 11-22 partial).
Cohort: 368 snapshot records. Inventory: 370 root .md workflows, 370 .lock.yml. 5 inventory workflows absent from the 11-17 snapshot; 3 snapshot records no longer in inventory.
Run-log access: HTTP 403 DIFC on `gh run view --log-failed` and paginated `gh run list`. Root causes are unverified. Only job and step metadata was used.

## Tiers (2026-11-17, sum 368)
- Healthy 14, Warning 4, Critical 5, Low-sample 186, Inactive 159 (unchanged from 11-21)
- Critical (11-17 history, current live status): ai-moderator (mostly succeeding), avenger (hourly succeeding), daily-go-test-parallelizer (still failing), dependabot-burner (last success 11-20), visual-regression-checker (conclusion failures 11-20 and 11-21)
- Warning: auditor (recovering, ~22 of 25 recent runs succeeded since 11-21 02:54Z), auto-triage-issues, mattpocock-skills-reviewer and pr-specification-reviewer (command gating, expected)

## Failing scheduled workflows (live, 11-20 to 11-22)
- code-scanning-dispatcher: 3 of 3 recent failures at safe_outputs step "Process trusted work queue controls" (runs 44984268988, 44967409332, 44945019199). Tracker filed this run.
- daily-action-setup-security-audit: failed 11-20, 11-21, 11-22 (conclusion "Handle agent failure"). Auto-filed #79984 open.
- daily-community-attribution: failed 11-20 to 11-22 (agent Execute Copilot CLI, conclusion). Auto-filed #79978 open.
- daily-model-inventory: failed 11-19 to 11-21 (conclusion). Five "reported incomplete" auto-issues in 6 days (#78483, #78869, #79375, #79623, #79956), all closed. No open tracker.
- daily-go-test-parallelizer: still failing after P1 #79686 closed COMPLETED on 11-21. Runs 44968274426 (11-22 00:46, failed agent and conclusion) and 44956859796 (11-21 22:27, failed conclusion). Open auto-issues #79921, #79968.
- smoke-work-queue-dispatcher: failed 11-21 and 11-22 (agent, safe_outputs, verify_smoke_dispatch). Auto-filed #79977 open.
- visual-regression-checker: conclusion failure runs 44943996341 (11-21 20:01) and 11-21 19:40. No open tracker.

## Tracker and expiry state
- #79418 (Auditor P1) and #79421 (create-issue expires:1d maintenance) closed NOT_PLANNED at 24h expiry, not by a human decision.
- #79686 (daily-go-test-parallelizer P1) closed COMPLETED 11-21 while failures continue. Fix attribution is not confirmed.
- Maintainer decision still needed on the expires:1d setting in workflow-health-manager.md.

## Actions this run
- Filed P1 tracker for code-scanning-dispatcher (untracked, 3 of 3 recent failures at the same step).
- Dashboard 2026-11-22 filed as the terminal safe-output (material delta: untracked failing workflows and recurring daily-go-test-parallelizer failures).

## Corrections to prior notes
- dependabot-burner: last success 11-20; "6 of 6 failed" note is stale.
- ai-moderator: no 24h failures; Critical tier reflects 11-17 history.
- pr-sous-chef: 11-19 to 11-20 burst (57 of 123 failed, per Agent Performance Analyzer) is not on any dashboard. Recent runs succeed. Not tracked; not escalated without log evidence.
