# Workflow Health Summary - 2026-11-26

Timestamp: 2026-11-26T04:38Z
Scored snapshot: metrics/daily/2026-11-26.json (collection_status partial, but full 24h Actions API pagination, 34 pages; 24h window 2026-11-25T02:56Z to 2026-11-26T02:56Z). Log coverage partial: 7 batches, 21:13Z to 02:52Z.
Cohort: 371 records = inventory 371 (0 gaps). Lock files 371 (0 unpaired).
Compile data gap: compile-validate.txt holds only the install log. Compile status rests on the lock-file pairing.
Run-log access: HTTP 403 DIFC on `gh run view --log-failed`. Root causes are unverified. Job metadata used.
Trend: 7d/30d unavailable. No consecutive complete snapshots since 11-17 (11-16, 11-18 onward partial).
Comparison with 11-17: non-comparable (cohort 368 vs 371). Do not describe as regression or recovery.

## Tiers (2026-11-26, sum 371)
- Healthy 18, Warning 6, Critical 2, Low-sample 214, Inactive 131
- Critical: code-scanning-dispatcher (0/6, recovered 04:16Z, run 45754141157); daily-credit-limit-test (1/2, intentional failure, `intentional-failure: true`, expected)
- Warning: daily-go-test-parallelizer (8/12), visual-regression-checker (2/3), ai-moderator (35/47, 63 skipped = trigger gating), smoke-work-queue-dispatcher (3/4), auto-triage-issues (14/18), auditor (19/24)
- Aggregate: 774/928 executed succeeded (83.4%); skipped 824; action_required 58 (reported separately)
- Low-sample tier hides daily 1-run workflows that fail every day: daily-action-setup-security-audit, daily-model-inventory. Use the live run check.

## Live findings (run check 11-22 to 11-26)
- code-scanning-dispatcher: 6 of 7 runs in snapshot window failed. Last run 04:16Z succeeded. Tracker #80956 open. Keep open until 24h green.
- daily-action-setup-security-audit: conclusion job fails daily 11-22 to 11-26 (5/5). Agent, detection, and safe_outputs succeed. Pattern "Handle agent failure". Prior #80926 closed COMPLETED/NOT_PLANNED. Auto-filed #81205 open (status comment this run; no duplicate P1).
- daily-model-inventory: failed 11-21, 11-22, 11-24, 11-25; success 11-23. Auto-filed #81181 open. Prior #80838 closed.
- daily-go-test-parallelizer: failure 11-26 02:30Z (run 45742891197). Open #81201, WIP #81231.
- visual-regression-checker: failure 11-25 09:54Z (run 45589869817). No open tracker.
- auditor: hourly, failed run 11-26 02:31Z (run 45742962399). Open auto-filed #81203, #81137, #81113, #81010, #80982. Root cause unverified.
- smoke-work-queue-dispatcher: failed 11-25 06:35 (run 45561762629). Open #81003.
- Budget guardrail: 21 "exceeded daily AI credits budget" issues since 11-24. Cascade tracker #80251 open.

## Tracker and expiry state
- #80001 closed NOT_PLANNED 11-23 at expiry. #80956 is the current code-scanning tracker.
- #79686 closed COMPLETED 11-21. Failures continue. Attribution stale.
- Maintainer decision still needed on expires:1d in workflow-health-manager.md.
- Dashboard 2026-11-25 (#80958) expires 04:40Z 11-26. Replaced by the 11-26 dashboard.

## Actions this run (emitted as safe outputs; not yet verified)
- Status comment on #81205 for daily-action-setup-security-audit: NOT posted (add_comment target is triggering; schedule run has no target). Pending.
- Status comment on #80956 for code-scanning-dispatcher recovery: NOT posted (same limit). Pending.
- Dashboard "Workflow Health Dashboard - 2026-11-26".

## Corrections to prior notes
- code-scanning-dispatcher: recovered 11-26 04:16Z (one green run). Not yet stable.
- daily-credit-limit-test: intentional failure. Do not file as P1.
- daily-action-setup-security-audit: 5 consecutive daily failures (live), not Low-sample. Prior 11-25 note cited only the 11-20 to 11-25 window.
- dependabot-burner: last success 11-20; "6 of 6 failed" note is stale.
- ai-moderator: recent runs succeed; the Critical tier reflects 11-17 history. 11-26 tier is Warning.
- #79686 closed COMPLETED 11-21 but failures continue; attribution stale.
- #20315 is closed; do not cite it as pending.
