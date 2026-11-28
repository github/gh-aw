# Workflow Health Manager — 2026-11-28 (snapshot 03:08Z)

- Snapshot: 2026-11-28T03:08Z; window 2026-11-27T02:52Z–2026-11-28T02:52Z. Collection partial: run counts from GitHub API fallback; logs, safe outputs, cost and tokens unavailable.
- Copilot Session Insights: 8 consecutive scheduled failures Nov 20–27 (last 06:45Z, SHA ca7ccb770f). Tracker #81538 closed. Fix PR #81699 merged 2026-11-27T17:25Z, after the last failing run. Fix unverified until the next scheduled run. No new P1 filed; comment on dashboard #81539.
- Architecture Guardian (#81536): latest run succeeded 11-27 14:14Z after 5 failures. Keep open until another green window.
- Daily Documentation Diagram (#81537): latest run succeeded 11-27 04:54Z after 7 failures. Keep open.
- smoke-work-queue-dispatcher (#81440): 3 consecutive successes since 11-27 12:31Z. Keep open.
- Expiry: `[aw]` failure trackers (e.g. #81495) auto-close NOT_PLANNED at 24h expiry. This is not a fix; maintainer decision pending.
- Stale attributions removed: #81205, #81181, #81201, #80956, #79686 are closed; do not cite as open fixes.
- Labels `priority-p1` and `type-failure` exist (earlier note was wrong).

## Prior run (2026-11-27T04:31Z), superseded
- Snapshot: 2026-11-27T02:53:28Z; 24h window 2026-11-26T02:50:11Z–2026-11-27T02:50:11Z. Collection partial: run counts use full-window paginated Actions API fallback; logs, safe outputs, engagement, cost and tokens are incomplete or unavailable.
- Inventory: 371 executable workflows, 371 corresponding locks, no missing locks. `compile-validate.txt` only reports installation; compilation success and warnings are unavailable, not inferred from locks.
- Measured non-command workflows: 304. Healthy 15, Warning 3, Critical 2, Low-sample 160, Inactive 124 (sum 304). Command/mention workflows: 67 unavailable for activated-run scoring; raw runs include expected command gating and do not identify activated runs. Prior dashboard is non-comparable because the measured cohort differs.
- Critical: auto-triage-issues 4/8 successful, tracker #81455; smoke-work-queue-dispatcher 2/4, tracker #81440. Warning: auditor 17/24 (#81203), daily-go-test-parallelizer 8/12, visual-regression-checker 3/4. These are execution-rate tiers, not root-cause diagnoses.
- Repeated failures despite low snapshot samples: Copilot Session Insights 7/7 scheduled failures Nov 20–26, activation `Check workflow lock file`; Daily Documentation Diagram 7/7 Nov 20–26, conclusion `Handle agent failure`; Architecture Guardian 5/5 weekday runs Nov 20 and 23–26, engine step failures. Three separate P1 issue creation requests accepted this run; actual issue numbers assigned on publication.
- Other repeated failures are tracked: security audit 8/8 Nov 20–27 (#81495); model inventory 6/7 Nov 20–26 (#81470); workflow audit 7/7 (#81459); issue clustering 7/7, prior different failed job #81001; BYOK Ollama 5/5 (#81463); cache analyzer 7/7 (#81436); ecosystem explorer 7/7 (#81395); formal verifier 7/7 (#81401). Failure boundaries do not prove common cause.
- code-scanning-dispatcher: 6/6 in snapshot, seven successes since 2026-11-26T04:16Z; previous tracker #80956 closed NOT_PLANNED. Do not retain old failure attribution. #81205 and #81181 are closed; current trackers above. #81201 closed, #81203 and cascade tracker #80251 open. #79686 is closed; ongoing parallelizer failures are not evidence its fix remains pending.
- 7-day and 30-day trends unavailable: Nov 21 and 24 daily snapshots missing; Nov 10 also missing for 30-day window, and several snapshots partial. MTBF unavailable without complete timestamped failure history. Failed-log access HTTP 403; error types and shared causation remain unverified. Safe-output quota, API usage and weekly runtime unavailable.
- Labels `priority-p1` and `type-failure` were not found in the enumerated labels; new issues use `workflow-health` and state priority in body. No repo source changes.
