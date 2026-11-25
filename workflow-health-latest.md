# Workflow Health Summary - 2026-11-25 (run 45547131214)

Timestamp: 2026-11-25T04:45Z
Scored snapshot: metrics/daily/2026-11-17.json (complete, 24h window 2026-11-16T02:33Z to 2026-11-17T02:33Z). latest.json (2026-11-25, ~6.4h) is partial and not scored. 7d/30d trend unavailable (no consecutive complete snapshots after 11-17).
Cohort: 368 snapshot records. Inventory: 371 root .md workflows, 371 .lock.yml (0 missing). 6 inventory workflows absent from the 11-17 snapshot; 3 snapshot records no longer in inventory.
Compile data gap: compile-validate.txt holds only the install log, not gh aw compile output. Compile status rests on the lock-file count.
Run-log access: HTTP 403 DIFC on `gh run view --log-failed`. Root causes are unverified. Job and step metadata used.

## Tiers (2026-11-17, sum 368)
- Healthy 14, Warning 4, Critical 5, Low-sample 186, Inactive 159 (unchanged from 11-22)
- Critical: daily-go-test-parallelizer (0/8), dependabot-burner (0/3, last success 11-20), ai-moderator (5/17, recent runs succeed), avenger (1/2, recent runs succeed), visual-regression-checker (1/2)
- Warning: auditor (12/19), auto-triage-issues (4/6), pr-specification-reviewer (27/36, command gating, expected), mattpocock-skills-reviewer (25/33, command gating, expected)

## Failing scheduled workflows (live, 11-20 to 11-25)
- code-scanning-dispatcher: 6 consecutive failures (11-24 08:18 to 11-25 04:16). Failing steps: safe_outputs "Process trusted work queue controls" (runs 45545696023, 45519103947) and activation "Snapshot work queue state" (runs 45478392904, 45434957095, 45392497231, 45354921822). P1 tracker filed this run. Prior P1 #80001 closed NOT_PLANNED 11-23 at expiry.
- daily-action-setup-security-audit: failed daily 11-20 to 11-25 (conclusion "Handle agent failure"). Open #80926. Earlier #79984 closed NOT_PLANNED.
- daily-model-inventory: 4 of last 6 failed. Open #80838.
- daily-go-test-parallelizer: failures 11-25 02:30 and 00:45 (run 45520576298 conclusion; run 45481002959 agent "Execute Codex CLI"). One success 11-24 22:27. Open #80845, #80769, #80760, #80731, #80921 (HTTP 429), #80803, #80952 (WIP).
- smoke-work-queue-dispatcher: failed 11-24, success 11-25.
- visual-regression-checker: mixed on 11-25 (successes plus failures and an action_required run). Open #80909, #80854.
- auditor: mixed on 11-25. Old P1 #79418 closed NOT_PLANNED at expiry.

## Tracker and expiry state
- #80001 (code-scanning-dispatcher P1) closed NOT_PLANNED 11-23 at expiry.
- #79686 (daily-go-test-parallelizer P1) closed COMPLETED 11-21. Failures continue, so the fix attribution is stale.
- Cascade tracker #80251 is OPEN (created 11-23, updated 11-25). 233 open issues, 41 labelled cascade-suspected. Large wave of [aw] failure issues on 11-24 and 11-25 (AIC budget exhaustion, engine driver crashes). Shared cause not verified.
- Maintainer decision still needed on expires:1d in workflow-health-manager.md.

## Actions this run
- Filed P1 tracker for code-scanning-dispatcher (untracked, 6 consecutive failures).
- Filed dashboard "Workflow Health Dashboard - 2026-11-25" (scored 11-17 snapshot, live findings unscored).

## Corrections to prior notes
- dependabot-burner: last success 11-20; "6 of 6 failed" note is stale.
- ai-moderator: recent runs succeed; the Critical tier reflects 11-17 history.
- #79686 closed COMPLETED 11-21 but failures continue; attribution stale.
- #20315 is closed; do not cite it as pending.
